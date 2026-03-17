# spank-lab

**Slap your laptop to steer Claude Code's behavior.**

A physical frustration feedback loop for AI coding assistants. Slap your MacBook when Claude does something dumb. The harder and more often you slap, the more cautious it becomes -- asking for confirmation, slowing down, double-checking assumptions.

## Architecture

```
                         spank-lab

  You slap laptop       accelerometer data        timestamped events
  +-----------+     +-------------------+     +--------------------+
  |  MacBook  | --> |  spank (Go CLI)   | --> |  ~/.spank/events   |
  +-----------+     +-------------------+     +--------------------+
                                                       |
                                                       v
                    +-------------------+     +--------------------+
                    |  score cache      | <-- |  vibe-check daemon |
                    |  ~/.spank/score   |     |  (Python)          |
                    +-------------------+     +--------------------+
                             |
                             v
                    +-------------------+     +--------------------+
                    |  PreToolUse hook  | --> |  Claude Code       |
                    |  (spank-claude)   |     |  (adjusts behavior)|
                    +-------------------+     +--------------------+
```

## Components

| Component | Language | Description |
|-----------|----------|-------------|
| **spank** | Go | Reads the accelerometer via `sudo powermetrics`, detects slaps, writes events |
| **mcp-spank** | Go | MCP server exposing slap data to Claude via tool calls |
| **vibe-check** | Python | Daemon that reads events, computes a rolling frustration score, writes to cache |
| **spank-claude** | Bash | PreToolUse hook script that reads the score cache and injects context into Claude Code |

## Quick Setup

```bash
# 1. Build the Go binaries
make

# 2. Start the slap detector (needs sudo for accelerometer access)
sudo ./spank/spank

# 3. Start the vibe-check daemon
python3 vibe-check/vibe_check.py &

# 4. Install the Claude Code hook
cp spank-claude ~/.claude/hooks/spank-claude.sh
chmod +x ~/.claude/hooks/spank-claude.sh
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `SPANK_EVENTS` | `~/.spank/events` | Path to the slap events file |
| `SPANK_SCORE_CACHE` | `~/.spank/score` | Path to the computed frustration score |
| `SPANK_BINARY` | `spank` | Path to the spank binary |

## Frustration Levels

| Level | Score Range | Claude's Behavior |
|-------|------------|-------------------|
| **calm** | < 1.0 | Normal operation |
| **frustrated** | 1.0 -- 3.0 | Double-checks assumptions, keeps responses concise |
| **hot** | 3.0 -- 5.0 | Asks before every non-trivial action, shorter responses |
| **angry** | > 5.0 | Minimal actions, asks for confirmation on everything |

## Requirements

- Apple Silicon MacBook (M2 or later)
- `sudo` access (required for `powermetrics` accelerometer data)
- Go 1.21+ (to build spank and mcp-spank)
- Python 3.10+ (for vibe-check)
