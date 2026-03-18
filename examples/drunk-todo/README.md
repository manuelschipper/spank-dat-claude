# drunk-todo

A todo app written by Claude Code in drunk mode.

This is what happens when you keep slapping your laptop while Claude writes code. Notice:

- `frank` — a variable name with no explanation (blackout-level naming)
- `# TODO: understand what i wrote here when sober`
- `# future me: i'm sorry`
- `# fix this when head stops pounding`
- `# ok so this part is where we... yeah`
- A function called `yolo()` that deletes everything with zero confirmation
- Empty-list messages like "clean slate baby. go touch grass."

The code actually works perfectly. That's the drunk mode promise — correct logic, questionable style.

## Usage

```bash
python3 todo.py add "buy milk" -p low
python3 todo.py add "fix the bug" -p high -d 2026-03-20
python3 todo.py ls
python3 todo.py done 0
python3 todo.py yolo    # nuclear option. no regrets. well maybe some regrets.
```
