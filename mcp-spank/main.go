// mcp-spank is an MCP (Model Context Protocol) server that wraps the spank
// accelerometer tool, exposing laptop slap detection as tools for LLMs.
//
// It spawns `spank --stdio` as a subprocess and bridges slap events into
// the MCP tool interface. Requires spank to be installed and sudo access
// for the accelerometer.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// --- Spank event types ---

type SlapEvent struct {
	Timestamp  string  `json:"timestamp"`
	SlapNumber int     `json:"slapNumber"`
	Amplitude  float64 `json:"amplitude"`
	Severity   string  `json:"severity"`
	File       string  `json:"file"`
}

// --- Event ring buffer ---

const maxEvents = 100

type EventStore struct {
	mu     sync.RWMutex
	cond   *sync.Cond
	events []SlapEvent
	total  int
}

func NewEventStore() *EventStore {
	es := &EventStore{}
	es.cond = sync.NewCond(&es.mu)
	return es
}

func (es *EventStore) Add(ev SlapEvent) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.events = append(es.events, ev)
	if len(es.events) > maxEvents {
		es.events = es.events[len(es.events)-maxEvents:]
	}
	es.total++
	es.cond.Broadcast()
}

func (es *EventStore) Recent(n int) []SlapEvent {
	es.mu.RLock()
	defer es.mu.RUnlock()
	if n <= 0 || n > len(es.events) {
		n = len(es.events)
	}
	start := len(es.events) - n
	out := make([]SlapEvent, n)
	copy(out, es.events[start:])
	return out
}

func (es *EventStore) Total() int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.total
}

func (es *EventStore) WaitForSlap(timeout time.Duration) (*SlapEvent, error) {
	es.mu.Lock()
	startTotal := es.total

	// Timer goroutine: broadcast after timeout so Wait() unblocks.
	timer := time.AfterFunc(timeout, func() {
		es.cond.Broadcast()
	})
	defer timer.Stop()

	for es.total == startTotal {
		es.cond.Wait()
		if es.total == startTotal {
			// Woken by timeout, not by a new event.
			es.mu.Unlock()
			return nil, fmt.Errorf("timeout: no slap detected within %v", timeout)
		}
	}

	ev := es.events[len(es.events)-1]
	es.mu.Unlock()
	return &ev, nil
}

// --- MCP JSON-RPC types ---

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// --- Tool definitions ---

var toolDefinitions = []map[string]interface{}{
	{
		"name":        "wait_for_slap",
		"description": "Block until a physical slap/hit is detected on the laptop. Returns the slap event with amplitude, severity, and timestamp. Use this for binary physical input — e.g., 'slap to confirm' or 'slap to deny'.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"timeout_seconds": map[string]interface{}{
					"type":        "number",
					"description": "Max seconds to wait for a slap (default: 30)",
					"default":     30,
				},
			},
		},
	},
	{
		"name":        "get_recent_slaps",
		"description": "Get recent slap events. Returns the last N slap events with amplitude, severity, and timing data. Useful for reading physical input patterns — e.g., counting slaps for multi-choice input.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{
					"type":        "number",
					"description": "Number of recent events to return (default: 5, max: 100)",
					"default":     5,
				},
			},
		},
	},
	{
		"name":        "get_slap_stats",
		"description": "Get slap statistics — total count, recent frequency, average amplitude. Useful for frustration detection or engagement monitoring.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
	},
}

// --- MCP Server ---

type MCPServer struct {
	store  *EventStore
	reader *bufio.Reader
	writer io.Writer
}

func NewMCPServer(store *EventStore, r io.Reader, w io.Writer) *MCPServer {
	return &MCPServer{
		store:  store,
		reader: bufio.NewReader(r),
		writer: w,
	}
}

func (s *MCPServer) writeResponse(resp interface{}) {
	// json.Marshal cannot fail here: resp is always MCPResponse or
	// MCPNotification — structs with string/int/map/slice fields, no
	// channels, funcs, or cycles.
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}

func (s *MCPServer) handleInitialize(id json.RawMessage) {
	s.writeResponse(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mcp-spank",
				"version": "0.1.0",
			},
		},
	})
}

func (s *MCPServer) handleToolsList(id json.RawMessage) {
	s.writeResponse(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"tools": toolDefinitions,
		},
	})
}

func (s *MCPServer) handleToolCall(id json.RawMessage, params json.RawMessage) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		s.writeResponse(MCPResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &MCPError{Code: -32602, Message: "invalid params"},
		})
		return
	}

	var result interface{}
	var toolErr error

	switch call.Name {
	case "wait_for_slap":
		result, toolErr = s.toolWaitForSlap(call.Arguments)
	case "get_recent_slaps":
		result, toolErr = s.toolGetRecentSlaps(call.Arguments)
	case "get_slap_stats":
		result, toolErr = s.toolGetSlapStats()
	default:
		s.writeResponse(MCPResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &MCPError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", call.Name)},
		})
		return
	}

	if toolErr != nil {
		s.writeResponse(MCPResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("Error: %s", toolErr.Error())},
				},
				"isError": true,
			},
		})
		return
	}

	text, _ := json.MarshalIndent(result, "", "  ")
	s.writeResponse(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": string(text)},
			},
		},
	})
}

func (s *MCPServer) toolWaitForSlap(args json.RawMessage) (interface{}, error) {
	var params struct {
		TimeoutSeconds float64 `json:"timeout_seconds"`
	}
	params.TimeoutSeconds = 30
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-spank: wait_for_slap: bad args: %v\n", err)
		}
	}
	if params.TimeoutSeconds <= 0 {
		params.TimeoutSeconds = 30
	}
	if params.TimeoutSeconds > 120 {
		params.TimeoutSeconds = 120
	}

	timeout := time.Duration(params.TimeoutSeconds * float64(time.Second))
	ev, err := s.store.WaitForSlap(timeout)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *MCPServer) toolGetRecentSlaps(args json.RawMessage) (interface{}, error) {
	var params struct {
		Count int `json:"count"`
	}
	params.Count = 5
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-spank: get_recent_slaps: bad args: %v\n", err)
		}
	}
	if params.Count <= 0 {
		params.Count = 5
	}
	if params.Count > maxEvents {
		params.Count = maxEvents
	}

	events := s.store.Recent(params.Count)
	return map[string]interface{}{
		"events": events,
		"total":  s.store.Total(),
	}, nil
}

func (s *MCPServer) toolGetSlapStats() (interface{}, error) {
	events := s.store.Recent(maxEvents)
	total := s.store.Total()

	stats := map[string]interface{}{
		"total_slaps": total,
		"buffered":    len(events),
	}

	if len(events) == 0 {
		stats["avg_amplitude"] = 0
		stats["max_amplitude"] = 0
		stats["slaps_last_60s"] = 0
		return stats, nil
	}

	var sumAmp, maxAmp float64
	cutoff := time.Now().Add(-60 * time.Second)
	recentCount := 0

	for _, ev := range events {
		sumAmp += ev.Amplitude
		if ev.Amplitude > maxAmp {
			maxAmp = ev.Amplitude
		}
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil && t.After(cutoff) {
			recentCount++
		}
	}

	stats["avg_amplitude"] = sumAmp / float64(len(events))
	stats["max_amplitude"] = maxAmp
	stats["slaps_last_60s"] = recentCount
	return stats, nil
}

func (s *MCPServer) Run() error {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			s.handleInitialize(req.ID)
		case "initialized":
			// notification, no response needed
		case "tools/list":
			s.handleToolsList(req.ID)
		case "tools/call":
			s.handleToolCall(req.ID, req.Params)
		case "ping":
			s.writeResponse(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{},
			})
		}
	}
}

// --- Event source: read from a JSONL file/pipe ---

// tailEvents reads newline-delimited JSON slap events from a file,
// tailing it for new lines. This lets spank run separately with sudo
// while mcp-spank reads its output.
func tailEvents(store *EventStore, path string) {
	for {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp-spank: waiting for event source %s...\n", path)
			time.Sleep(1 * time.Second)
			continue
		}

		// Seek to end — we only want new events
		f.Seek(0, io.SeekEnd)

		scanner := bufio.NewScanner(f)
		for {
			if scanner.Scan() {
				line := scanner.Text()
				var ev SlapEvent
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					continue
				}
				if ev.SlapNumber == 0 {
					continue
				}
				store.Add(ev)
				fmt.Fprintf(os.Stderr, "mcp-spank: slap #%d amp=%.4f %s\n", ev.SlapNumber, ev.Amplitude, ev.Severity)
			} else {
				// No new data, poll
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

// --- Spank subprocess bridge (direct spawn, needs sudo) ---

func startSpankBridge(store *EventStore, spankBinary string) error {
	cmd := exec.Command("sudo", spankBinary, "--stdio", "--min-amplitude", "0.03")
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start spank: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			var ev SlapEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if ev.SlapNumber == 0 {
				fmt.Fprintf(os.Stderr, "spank: %s\n", line)
				continue
			}
			store.Add(ev)
			fmt.Fprintf(os.Stderr, "mcp-spank: slap #%d amp=%.4f %s\n", ev.SlapNumber, ev.Amplitude, ev.Severity)
		}
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-spank: spank exited: %v\n", err)
		}
	}()

	return nil
}

const defaultEventFile = "/tmp/spank-events.jsonl"

func main() {
	mode := "pipe" // default: read from event file
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--dry-run":
			mode = "dry-run"
		case "--spawn":
			mode = "spawn"
		case "--pipe":
			mode = "pipe"
		}
	}

	store := NewEventStore()

	switch mode {
	case "dry-run":
		fmt.Fprintf(os.Stderr, "mcp-spank: dry-run mode (fake events)\n")
		store.Add(SlapEvent{
			Timestamp:  time.Now().Add(-2 * time.Second).Format(time.RFC3339Nano),
			SlapNumber: 1,
			Amplitude:  0.085,
			Severity:   "VIBRATION",
			File:       "audio/pain/03_Hey_that_hurts.mp3",
		})
		store.Add(SlapEvent{
			Timestamp:  time.Now().Add(-1 * time.Second).Format(time.RFC3339Nano),
			SlapNumber: 2,
			Amplitude:  0.142,
			Severity:   "VIBRATION",
			File:       "audio/pain/08_Yowch.mp3",
		})

	case "spawn":
		spankBinary := os.Getenv("SPANK_BINARY")
		if spankBinary == "" {
			var err error
			spankBinary, err = exec.LookPath("spank")
			if err != nil {
				fmt.Fprintf(os.Stderr, "mcp-spank: spank not found. Set SPANK_BINARY or add to PATH.\n")
				os.Exit(1)
			}
		}
		if err := startSpankBridge(store, spankBinary); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-spank: failed to start spank: %v\n", err)
			os.Exit(1)
		}

	case "pipe":
		eventFile := os.Getenv("SPANK_EVENTS")
		if eventFile == "" {
			eventFile = defaultEventFile
		}
		fmt.Fprintf(os.Stderr, "mcp-spank: reading events from %s\n", eventFile)
		fmt.Fprintf(os.Stderr, "mcp-spank: start spank in another terminal:\n")
		fmt.Fprintf(os.Stderr, "  sudo spank --stdio --min-amplitude 0.03 >> %s\n", eventFile)
		go tailEvents(store, eventFile)
	}

	server := NewMCPServer(store, os.Stdin, os.Stdout)
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-spank: server error: %v\n", err)
		os.Exit(1)
	}
}
