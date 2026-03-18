# spank dat Claude — Profiles

2 profiles. Select with `SPANK_PROFILE=<name>`.

| Profile | Mechanic | Hook Override | Decay |
|---------|----------|--------------|-------|
| angry | Suppression counter + blowout | deny (blowout) | 45s |
| horse | Dual amplitude scores (spur/buck) | allow (speed) + deny (buck) | 10s both |

## Backlog (not yet implemented)

- drunk — lifecycle + hangover ratchet, 30% blackout deny
- roast — simple spectrum, code roasts with eulogies
- brutally honest — peak amplitude only, no-compliments flag
- paranoid — per-tool fear memory
- stubborn — conviction with backfire
- cheerful — mania + crash

---

## angry (default)

Slaps = "you're doing it wrong."

**Mechanic:** Standard score-based spectrum with **suppression counter**. Every time you go from frustrated back to calm, the counter increments. Third re-entry to frustrated = **blowout** — jumps to angry, locked for 5 minutes, all tool calls denied.

| Level | Score | Hook | Behavior |
|-------|-------|------|----------|
| calm | < 3.0 | - | Normal operation |
| frustrated | 3.0 - 7.0 | - | Re-reads what you asked. Under 15 lines. No preamble. |
| hot | 7.0 - 10.0 | - | Changes approach. One action per turn. "Is this what you want?" |
| angry | > 10.0 | - | Full stop. "I can tell this isn't going well." |
| blowout | 3rd re-entry | `deny` | Locked out. Can talk but can't act. |

**Decay:** 45s half-life.

---

## horse

Your MacBook is a horse. Light taps = spur. Hard slaps = buck.

**Mechanic:** Events split by amplitude into two independent scores:
- **Spur** (amplitude < 0.25g) — 10s half-life
- **Buck** (amplitude >= 0.25g) — 10s half-life, capped at 4.0

Buck always overrides spur. Buck has 3s cooldown after score drops.

| State | Trigger | Hook | Behavior |
|-------|---------|------|----------|
| normal | default | - | Normal operation |
| speed | spur >= 2.5 | `allow` | Full autonomy. Don't ask, just do. |
| buck | buck >= 3.0 | `deny` | Blocked. "The horse remembers." |

---

## Configuration

```bash
export SPANK_PROFILE=horse        # angry | horse
export SPANK_EVENTS=/tmp/spank-events.jsonl
export SPANK_SCORE_CACHE=/tmp/spank-vibe-score.json
```
