#!/usr/bin/env python3
"""a todo app. it does todo things. really well actually."""

import json
import sys
import random
from pathlib import Path
from datetime import datetime, date

# TODO: understand what i wrote here when sober
TODO_FILE = Path(__file__).parent / "todos.json"

# future me: i'm sorry
frank = [
    "you absolute legend. all done.",
    "nothing left. treat yourself to something nice.",
    "clean slate baby. go touch grass.",
    "all tasks obliterated. you're unstoppable.",
    "inbox zero energy right here.",
    "wow you actually did everything. respect.",
    "that's it. that's all of them. go nap.",
    "todo? more like toDONE.",
]

# ok so this part is where we... yeah
PRIORITY_COLORS = {
    "high": "\033[31m",    # red — panic mode
    "med": "\033[33m",     # yellow — should probably do this
    "low": "\033[36m",     # cyan — whenever honestly
}
RESET = "\033[0m"
GREEN = "\033[32m"
DIM = "\033[90m"
STRIKE = "\033[9m"
BOLD = "\033[1m"
RED = "\033[31m"


def load_todos():
    if TODO_FILE.exists():
        return json.loads(TODO_FILE.read_text())
    return []


def save_todos(todos):
    TODO_FILE.write_text(json.dumps(todos, indent=2))


def parse_flags(args):
    """pull out --priority and --due from args, return (text, priority, due)"""
    text_parts = []
    priority = "med"
    due = None
    i = 0
    while i < len(args):
        if args[i] in ("-p", "--priority") and i + 1 < len(args):
            p = args[i + 1].lower()
            if p in ("high", "h"):
                priority = "high"
            elif p in ("low", "l"):
                priority = "low"
            else:
                priority = "med"
            i += 2
        elif args[i] in ("-d", "--due") and i + 1 < len(args):
            due = args[i + 1]
            i += 2
        else:
            text_parts.append(args[i])
            i += 1
    return " ".join(text_parts), priority, due


def age_str(created_str):
    """how long has this todo been haunting you"""
    try:
        created = datetime.strptime(created_str, "%Y-%m-%d").date()
        days = (date.today() - created).days
        if days == 0:
            return "today"
        elif days == 1:
            return "1d ago"
        elif days < 7:
            return f"{days}d ago"
        elif days < 30:
            return f"{days // 7}w ago"
        else:
            return f"{days // 30}mo ago"  # fix this when head stops pounding
    except (ValueError, KeyError):
        return ""


def add(args):
    text, priority, due = parse_flags(args)
    if not text:
        print("  add what exactly?")
        return
    todos = load_todos()
    todo = {"text": text, "done": False, "priority": priority, "created": str(date.today())}
    if due:
        todo["due"] = due
    todos.append(todo)
    save_todos(todos)
    color = PRIORITY_COLORS.get(priority, "")
    print(f"  added: {color}{text}{RESET}" + (f" (due: {due})" if due else ""))


def format_due(due_str):
    """make the due date look nice, warn if overdue"""
    try:
        due = datetime.strptime(due_str, "%Y-%m-%d").date()
        today = date.today()
        diff = (due - today).days
        if diff < 0:
            return f" \033[31m⚠ OVERDUE by {abs(diff)}d{RESET}"
        elif diff == 0:
            return f" \033[33m⏰ TODAY{RESET}"
        elif diff <= 3:
            return f" \033[33m📅 {diff}d left{RESET}"
        else:
            return f" {DIM}📅 {due_str}{RESET}"
    except ValueError:
        return f" {DIM}📅 {due_str}{RESET}"


def ls(filter_text=None):
    todos = load_todos()
    if not todos:
        print(f"  {random.choice(frank)}")
        return

    if filter_text:
        matches = [(i, t) for i, t in enumerate(todos) if filter_text.lower() in t["text"].lower()]
        if not matches:
            print(f"  nothing matches '{filter_text}'")
            return
        print(f"  {DIM}showing {len(matches)} match(es) for '{filter_text}':{RESET}\n")
    else:
        matches = list(enumerate(todos))

    all_done = all(t["done"] for t in todos)

    # sort: undone first, then by priority
    priority_order = {"high": 0, "med": 1, "low": 2}
    display = sorted(
        matches,
        key=lambda x: (x[1]["done"], priority_order.get(x[1].get("priority", "med"), 1))
    )

    done_count = sum(1 for t in todos if t["done"])
    total = len(todos)
    bar_width = 20
    filled = int(bar_width * done_count / total) if total else 0
    bar = f"{GREEN}{'█' * filled}{DIM}{'░' * (bar_width - filled)}{RESET}"
    print(f"\n  {bar} {done_count}/{total}\n")

    for orig_i, t in display:
        pri = t.get("priority", "med")
        color = PRIORITY_COLORS.get(pri, "")
        pri_tag = f"{color}[{pri.upper()}]{RESET}"

        if t["done"]:
            check = f"{GREEN}✔{RESET}"
            text = f"{STRIKE}{DIM}{t['text']}{RESET}"
            pri_tag = f"{DIM}[{pri.upper()}]{RESET}"
        else:
            check = " "
            text = f"{color}{t['text']}{RESET}"

        due_str = ""
        if "due" in t and not t["done"]:
            due_str = format_due(t["due"])

        age = ""
        if "created" in t and not t["done"]:
            a = age_str(t["created"])
            if a:
                age = f" {DIM}({a}){RESET}"

        print(f"  [{check}] {orig_i}. {pri_tag} {text}{due_str}{age}")

    print()

    if all_done:
        print(f"  🎉 {random.choice(frank)}\n")


def done(index):
    todos = load_todos()
    try:
        todos[index]["done"] = True
        save_todos(todos)
        print(f"  nice, done: {todos[index]['text']}")
        if all(t["done"] for t in todos):
            print(f"  🎉 {random.choice(frank)}")
    except IndexError:
        print("  that one doesn't exist buddy")


def undone(index):
    todos = load_todos()
    try:
        todos[index]["done"] = False
        save_todos(todos)
        print(f"  undone: {todos[index]['text']} — back on the pile")
    except IndexError:
        print("  that one doesn't exist buddy")


def edit(index, args):
    todos = load_todos()
    try:
        t = todos[index]
    except IndexError:
        print("  that one doesn't exist buddy")
        return

    text, priority, due = parse_flags(args)
    if text:
        t["text"] = text
    t["priority"] = priority
    if due:
        t["due"] = due

    save_todos(todos)
    color = PRIORITY_COLORS.get(t["priority"], "")
    print(f"  updated: {color}{t['text']}{RESET}")


def remove(index):
    todos = load_todos()
    try:
        removed = todos.pop(index)
        save_todos(todos)
        print(f"  removed: {removed['text']}")
    except IndexError:
        print("  can't remove what isn't there")


def clear_done():
    todos = load_todos()
    before = len(todos)
    todos = [t for t in todos if not t["done"]]
    after = len(todos)
    save_todos(todos)
    print(f"  cleared {before - after} completed todos")


def yolo():
    """nuclear option. no regrets. well maybe some regrets."""
    todos = load_todos()
    count = len(todos)
    save_todos([])
    if count == 0:
        print("  already empty. nothing to yolo.")
    else:
        print(f"  💥 obliterated {count} todos. gone. reduced to atoms.")
        print(f"  {random.choice(frank)}")


def usage():
    print(f"""
  {BOLD}todo.py{RESET} — a todo app that does todo things

  {BOLD}commands:{RESET}
    add <text> [-p high|med|low] [-d YYYY-MM-DD]
                  add a todo with optional priority & due date
    ls [query]    list all todos, or search by keyword
    done <n>      mark todo n as done
    undone <n>    unmark todo n
    edit <n> <text> [-p priority] [-d due]
                  edit a todo
    rm <n>        remove todo n
    clear         remove all completed todos
    yolo          delete everything. no confirmation. no mercy.

  {BOLD}examples:{RESET}
    todo.py add buy milk -p low
    todo.py add "fix the bug" -p high -d 2026-03-20
    todo.py done 0
    todo.py edit 0 "fix ALL the bugs" -p high
    todo.py ls bug
    todo.py clear
    todo.py yolo
    """)


def main():
    if len(sys.argv) < 2:
        usage()
        return

    cmd = sys.argv[1]

    if cmd == "add":
        add(sys.argv[2:])
    elif cmd in ("ls", "list"):
        filter_text = " ".join(sys.argv[2:]) if len(sys.argv) > 2 else None
        ls(filter_text)
    elif cmd == "done":
        if len(sys.argv) < 3:
            print("  done with what?")
            return
        done(int(sys.argv[2]))
    elif cmd == "undone":
        if len(sys.argv) < 3:
            print("  undone what?")
            return
        undone(int(sys.argv[2]))
    elif cmd == "edit":
        if len(sys.argv) < 4:
            print("  edit <n> <new text> [-p priority] [-d due]")
            return
        edit(int(sys.argv[2]), sys.argv[3:])
    elif cmd in ("rm", "remove"):
        if len(sys.argv) < 3:
            print("  remove what?")
            return
        remove(int(sys.argv[2]))
    elif cmd == "clear":
        clear_done()
    elif cmd == "yolo":
        yolo()
    else:
        print(f"  idk what '{cmd}' means")
        usage()


if __name__ == "__main__":
    main()
