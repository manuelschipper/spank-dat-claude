package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// EventStore operations
// ---------------------------------------------------------------------------

func TestEventStore_AddAndRecent(t *testing.T) {
	es := NewEventStore()

	es.Add(SlapEvent{SlapNumber: 1, Amplitude: 0.1})
	es.Add(SlapEvent{SlapNumber: 2, Amplitude: 0.2})
	es.Add(SlapEvent{SlapNumber: 3, Amplitude: 0.3})

	got := es.Recent(2)
	if len(got) != 2 {
		t.Fatalf("Recent(2) returned %d events, want 2", len(got))
	}
	// Should return the last 2 events in order.
	if got[0].SlapNumber != 2 || got[1].SlapNumber != 3 {
		t.Errorf("Recent(2) = slaps %d,%d; want 2,3", got[0].SlapNumber, got[1].SlapNumber)
	}

	// Recent(0) should return all.
	all := es.Recent(0)
	if len(all) != 3 {
		t.Fatalf("Recent(0) returned %d events, want 3", len(all))
	}
}

func TestEventStore_RingBufferEviction(t *testing.T) {
	es := NewEventStore()

	for i := 1; i <= maxEvents+1; i++ {
		es.Add(SlapEvent{SlapNumber: i, Amplitude: float64(i)})
	}

	all := es.Recent(0)
	if len(all) != maxEvents {
		t.Fatalf("buffer has %d events after %d adds, want %d", len(all), maxEvents+1, maxEvents)
	}

	// First event should have been evicted; oldest remaining is #2.
	if all[0].SlapNumber != 2 {
		t.Errorf("oldest event SlapNumber = %d, want 2", all[0].SlapNumber)
	}
	if all[len(all)-1].SlapNumber != maxEvents+1 {
		t.Errorf("newest event SlapNumber = %d, want %d", all[len(all)-1].SlapNumber, maxEvents+1)
	}
}

func TestEventStore_Total(t *testing.T) {
	es := NewEventStore()

	for i := 0; i < maxEvents+5; i++ {
		es.Add(SlapEvent{SlapNumber: i + 1})
	}

	if got := es.Total(); got != maxEvents+5 {
		t.Errorf("Total() = %d, want %d", got, maxEvents+5)
	}
}

// ---------------------------------------------------------------------------
// WaitForSlap
// ---------------------------------------------------------------------------

func TestWaitForSlap_ImmediateEvent(t *testing.T) {
	es := NewEventStore()

	go func() {
		time.Sleep(20 * time.Millisecond)
		es.Add(SlapEvent{SlapNumber: 1, Amplitude: 0.5, Severity: "VIBRATION"})
	}()

	ev, err := es.WaitForSlap(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForSlap returned error: %v", err)
	}
	if ev.Amplitude != 0.5 {
		t.Errorf("got amplitude %f, want 0.5", ev.Amplitude)
	}
}

func TestWaitForSlap_Timeout(t *testing.T) {
	es := NewEventStore()

	_, err := es.WaitForSlap(100 * time.Millisecond)
	if err == nil {
		t.Fatal("WaitForSlap should have returned a timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %q, want it to contain 'timeout'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MCP protocol round-trips
// ---------------------------------------------------------------------------

// sendMCP builds a newline-delimited JSON-RPC request string.
func mcpRequest(id int, method string, params interface{}) string {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		p, _ := json.Marshal(params)
		req["params"] = json.RawMessage(p)
	}
	b, _ := json.Marshal(req)
	return string(b) + "\n"
}

func readResponse(t *testing.T, line string) MCPResponse {
	t.Helper()
	var resp MCPResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("failed to parse response %q: %v", line, err)
	}
	return resp
}

func TestMCP_Initialize(t *testing.T) {
	input := mcpRequest(1, "initialize", nil)
	var out bytes.Buffer

	store := NewEventStore()
	srv := NewMCPServer(store, strings.NewReader(input), &out)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	resp := readResponse(t, out.String())

	var idNum int
	if err := json.Unmarshal(resp.ID, &idNum); err != nil {
		t.Fatalf("bad response id: %v", err)
	}
	if idNum != 1 {
		t.Errorf("response id = %d, want 1", idNum)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]interface{})
	if info["name"] != "mcp-spank" {
		t.Errorf("serverInfo.name = %v, want mcp-spank", info["name"])
	}
}

func TestMCP_ToolsList(t *testing.T) {
	input := mcpRequest(1, "tools/list", nil)
	var out bytes.Buffer

	store := NewEventStore()
	srv := NewMCPServer(store, strings.NewReader(input), &out)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	resp := readResponse(t, out.String())
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools is not a list")
	}
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		tm, _ := tool.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	for _, want := range []string{"wait_for_slap", "get_recent_slaps", "get_slap_stats"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestMCP_ToolCallGetSlapStats(t *testing.T) {
	store := NewEventStore()
	ts := time.Now().Format(time.RFC3339Nano)
	store.Add(SlapEvent{SlapNumber: 1, Amplitude: 0.1, Severity: "VIBRATION", Timestamp: ts})
	store.Add(SlapEvent{SlapNumber: 2, Amplitude: 0.3, Severity: "VIBRATION", Timestamp: ts})

	input := mcpRequest(1, "tools/call", map[string]interface{}{
		"name":      "get_slap_stats",
		"arguments": map[string]interface{}{},
	})

	var out bytes.Buffer
	srv := NewMCPServer(store, strings.NewReader(input), &out)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	resp := readResponse(t, out.String())
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("missing content in response")
	}
	first := content[0].(map[string]interface{})
	text := first["text"].(string)

	var stats map[string]interface{}
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("failed to parse stats JSON: %v", err)
	}
	if stats["total_slaps"].(float64) != 2 {
		t.Errorf("total_slaps = %v, want 2", stats["total_slaps"])
	}
	if stats["max_amplitude"].(float64) != 0.3 {
		t.Errorf("max_amplitude = %v, want 0.3", stats["max_amplitude"])
	}
	if stats["avg_amplitude"].(float64) != 0.2 {
		t.Errorf("avg_amplitude = %v, want 0.2", stats["avg_amplitude"])
	}
}

// ---------------------------------------------------------------------------
// Multi-request round-trip (initialize + tools/list in one stream)
// ---------------------------------------------------------------------------

func TestMCP_MultiRequest(t *testing.T) {
	input := mcpRequest(1, "initialize", nil) +
		mcpRequest(2, "tools/list", nil)

	var out bytes.Buffer
	store := NewEventStore()
	srv := NewMCPServer(store, strings.NewReader(input), &out)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2", len(lines))
	}

	// First response: initialize
	r1 := readResponse(t, lines[0])
	result1, _ := r1.Result.(map[string]interface{})
	if result1["protocolVersion"] != "2024-11-05" {
		t.Errorf("first response missing protocolVersion")
	}

	// Second response: tools/list
	r2 := readResponse(t, lines[1])
	result2, _ := r2.Result.(map[string]interface{})
	tools, _ := result2["tools"].([]interface{})
	if len(tools) != 3 {
		t.Errorf("tools/list returned %d tools, want 3", len(tools))
	}
}

// ---------------------------------------------------------------------------
// toolGetSlapStats direct call with known amplitudes
// ---------------------------------------------------------------------------

func TestToolGetSlapStats_Calculation(t *testing.T) {
	store := NewEventStore()
	ts := time.Now().Format(time.RFC3339Nano)

	amplitudes := []float64{0.10, 0.20, 0.30, 0.40, 0.50}
	for i, amp := range amplitudes {
		store.Add(SlapEvent{
			SlapNumber: i + 1,
			Amplitude:  amp,
			Severity:   "VIBRATION",
			Timestamp:  ts,
		})
	}

	srv := NewMCPServer(store, strings.NewReader(""), &bytes.Buffer{})
	result, err := srv.toolGetSlapStats()
	if err != nil {
		t.Fatalf("toolGetSlapStats: %v", err)
	}

	stats, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}

	assertFloat := func(key string, want float64) {
		t.Helper()
		got, ok := stats[key]
		if !ok {
			t.Errorf("missing key %q", key)
			return
		}
		gf, ok := got.(float64)
		if !ok {
			t.Errorf("%s is %T, not float64", key, got)
			return
		}
		if fmt.Sprintf("%.4f", gf) != fmt.Sprintf("%.4f", want) {
			t.Errorf("%s = %.4f, want %.4f", key, gf, want)
		}
	}

	if stats["total_slaps"] != 5 {
		t.Errorf("total_slaps = %v, want 5", stats["total_slaps"])
	}
	assertFloat("avg_amplitude", 0.30)
	assertFloat("max_amplitude", 0.50)

	// All events have a recent timestamp, so slaps_last_60s should be 5.
	if stats["slaps_last_60s"] != 5 {
		t.Errorf("slaps_last_60s = %v, want 5", stats["slaps_last_60s"])
	}
}
