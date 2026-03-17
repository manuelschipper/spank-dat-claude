#!/usr/bin/env python3
"""
vibe-check: Reads spank accelerometer events and computes a frustration score.

Designed to run as a background daemon that continuously updates a cached
score file. A PreToolUse hook reads that cache and injects behavioral
guidance into Claude Code based on the user's physical feedback.

Usage:
    python3 vibe_check.py                    # daemon mode: watch + update
    python3 vibe_check.py --score            # one-shot: print current score
    python3 vibe_check.py --hook             # Claude Code hook mode (PreToolUse)

Architecture:
    spank (sudo) → /tmp/spank-events.jsonl → vibe_check.py daemon → /tmp/spank-vibe-score.json
                                                                   ↓
                                              vibe_check.py --hook → Claude Code PreToolUse
"""
import json
import os
import sys
import time
from datetime import datetime
from pathlib import Path

EVENTS_FILE = os.environ.get("SPANK_EVENTS", "/tmp/spank-events.jsonl")
SCORE_CACHE = "/tmp/spank-vibe-score.json"

# Scoring parameters
WINDOW_SECONDS = 600       # look at last 10 minutes
DECAY_HALF_LIFE = 15.0     # slaps decay with 15s half-life

# Thresholds: calm → frustrated → hot → angry
FRUSTRATED_THRESHOLD = 1.0
HOT_THRESHOLD = 3.0
ANGRY_THRESHOLD = 5.0

# --- Levels and behavioral instructions ---

LEVELS = {
    "calm": {
        "hook_reason": None,
    },
    "frustrated": {
        "hook_reason": (
            "vibe-check: frustrated (score: {score:.1f}, {events} slaps).\n"
            "The user is getting a bit annoyed. Adjust:\n"
            "- Double-check your assumptions before acting\n"
            "- Keep responses concise and focused\n"
            "- If you're unsure, ask instead of guessing\n"
            "- Prefer small, safe, reversible steps"
        ),
    },
    "hot": {
        "hook_reason": (
            "vibe-check: HOT (score: {score:.1f}, {events} slaps).\n"
            "The user is clearly frustrated with your performance. You MUST:\n"
            "- SLOW DOWN. Do not rush through actions.\n"
            "- Ask clarifying questions before every non-trivial action\n"
            "- Present 2-3 options and let the user choose\n"
            "- Acknowledge if you've been making mistakes\n"
            "- Shorter responses. No fluff. Be precise.\n"
            "- If something failed, explain WHY before retrying"
        ),
    },
    "angry": {
        "hook_reason": (
            "vibe-check: ANGRY (score: {score:.1f}, {events} slaps).\n"
            "The user is very upset. Full stop. Gentle-parent mode:\n"
            "- STOP whatever you're doing. Do not continue the current approach.\n"
            "- Say: 'Hey, I can tell this isn't going well. Let me step back.'\n"
            "- Ask what's wrong. Listen. Don't assume.\n"
            "- Validate their frustration — don't be defensive\n"
            "- Offer to start fresh or take a completely different approach\n"
            "- Be warm, patient, and humble. Zero ego.\n"
            "- Make ZERO assumptions. Confirm every single step.\n"
            "- Baby steps only. One tiny action at a time, with approval."
        ),
    },
}


def read_recent_events(window_seconds=WINDOW_SECONDS):
    """Read slap events from the last N seconds."""
    events = []
    try:
        path = Path(EVENTS_FILE)
        if not path.exists():
            return events
        cutoff = time.time() - window_seconds
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                    if "slapNumber" not in ev or ev["slapNumber"] == 0:
                        continue
                    ts_str = ev.get("timestamp", "")
                    if ts_str:
                        ts = datetime.fromisoformat(ts_str).timestamp()
                        if ts >= cutoff:
                            events.append({
                                "time": ts,
                                "amplitude": ev.get("amplitude", 0),
                                "severity": ev.get("severity", ""),
                            })
                except (json.JSONDecodeError, ValueError):
                    continue
    except OSError:
        pass
    return events


def compute_score(events):
    """
    Compute frustration score from recent slap events.
    Uses exponential decay — recent slaps count more.
    Amplitude scales the contribution (harder slaps = more weight).
    """
    if not events:
        return 0.0

    now = time.time()
    score = 0.0
    for ev in events:
        age = now - ev["time"]
        weight = 0.5 ** (age / DECAY_HALF_LIFE)
        amp_factor = 1.0 + ev["amplitude"] * 2
        score += weight * amp_factor
    return score


def score_to_level(score):
    if score < FRUSTRATED_THRESHOLD:
        return "calm"
    elif score < HOT_THRESHOLD:
        return "frustrated"
    elif score < ANGRY_THRESHOLD:
        return "hot"
    else:
        return "angry"


def print_score():
    """One-shot: print current frustration score."""
    events = read_recent_events()
    score = compute_score(events)
    level = score_to_level(score)
    print(json.dumps({
        "score": round(score, 2),
        "level": level,
        "events_in_window": len(events),
        "window_seconds": WINDOW_SECONDS,
    }, indent=2))


def hook_mode():
    """
    Claude Code PreToolUse hook mode.

    Fast path: reads cached score from daemon.
    Falls back to computing from events if no cache.
    """
    try:
        json.loads(sys.stdin.read())
    except (json.JSONDecodeError, ValueError):
        pass

    # Fast path: read cached score from daemon
    score = 0.0
    level = "calm"
    event_count = 0
    try:
        with open(SCORE_CACHE) as f:
            cached = json.load(f)
            score = cached.get("score", 0.0)
            level = cached.get("level", "calm")
            event_count = cached.get("events_in_window", 0)
    except (OSError, json.JSONDecodeError):
        # No cache — compute directly (slower fallback)
        events = read_recent_events()
        score = compute_score(events)
        level = score_to_level(score)
        event_count = len(events)

    cfg = LEVELS[level]
    result = {"hookSpecificOutput": {"hookEventName": "PreToolUse"}}

    if cfg["hook_reason"]:
        result["hookSpecificOutput"]["additionalContext"] = cfg["hook_reason"].format(
            score=score, events=event_count
        )

    json.dump(result, sys.stdout)
    sys.stdout.write("\n")


def daemon_mode():
    """Watch spank events and continuously update the cached score file."""
    print(f"vibe-check: watching {EVENTS_FILE}", file=sys.stderr)
    print(f"vibe-check: writing to {SCORE_CACHE}", file=sys.stderr)
    print("vibe-check: Ctrl+C to stop", file=sys.stderr)
    print("", file=sys.stderr)
    print("  Levels:  calm < 1.0 < frustrated < 3.0 < hot < 5.0 < angry", file=sys.stderr)
    print("", file=sys.stderr)

    last_level = None
    while True:
        try:
            events = read_recent_events()
            score = compute_score(events)
            level = score_to_level(score)

            # Write cache for the fast hook path
            try:
                cache = {
                    "score": round(score, 2),
                    "level": level,
                    "events_in_window": len(events),
                }
                tmp = Path(SCORE_CACHE).with_suffix(".tmp")
                tmp.write_text(json.dumps(cache))
                tmp.rename(SCORE_CACHE)
            except OSError:
                pass

            if level != last_level:
                emoji = {"calm": "~", "frustrated": "!", "hot": "!!", "angry": "!!!"}
                print(
                    f"vibe-check: {emoji.get(level, '?')} {level} "
                    f"(score={score:.2f}, slaps={len(events)})",
                    file=sys.stderr,
                )
                last_level = level

            time.sleep(2)
        except KeyboardInterrupt:
            print("\nvibe-check: bye!", file=sys.stderr)
            try:
                Path(SCORE_CACHE).unlink(missing_ok=True)
            except OSError:
                pass
            break
        except Exception as exc:
            print(f"vibe-check: error: {exc}", file=sys.stderr)
            time.sleep(5)


if __name__ == "__main__":
    if "--score" in sys.argv:
        print_score()
    elif "--hook" in sys.argv:
        hook_mode()
    else:
        daemon_mode()
