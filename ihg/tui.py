"""curses UI for browsing and running history entries."""

from __future__ import annotations

import curses
from difflib import SequenceMatcher
from typing import List, Optional, Tuple

from ihg.filters import apply_text_filter
from ihg.run import run_selected

Pair = Tuple[str, int]  # (command, recency_rank)


def _visible_pairs(
    base_commands: List[str],
    filter_text: str,
    sort_mode: Optional[str],
) -> List[Pair]:
    n = len(base_commands)
    rec = list(range(n))
    cmds, r = apply_text_filter(base_commands, rec, filter_text)
    pairs: List[Pair] = list(zip(cmds, r))
    if sort_mode == "asc":
        pairs.sort(key=lambda p: p[0].casefold())
    elif sort_mode == "desc":
        pairs.sort(key=lambda p: p[0].casefold(), reverse=True)
    return pairs


def _cluster_commands(commands: List[str], limit: int = 200, threshold: float = 0.55) -> List[List[str]]:
    items = commands[:limit]
    clusters: List[List[str]] = []
    for c in items:
        placed = False
        for g in clusters:
            rep = g[0]
            if SequenceMatcher(None, c, rep).quick_ratio() >= threshold:
                g.append(c)
                placed = True
                break
        if not placed:
            clusters.append([c])
    return clusters


def _parse_colon(buf: str) -> Optional[str]:
    """
    Return action name: quit, sort_asc, sort_desc, group, digit_N, unknown.
    buf must include leading ':'.
    """
    if not buf.startswith(":"):
        return None
    body = buf[1:].strip()
    if body == "q":
        return "quit"
    if body == "s":
        return "sort_asc"
    if body == "S":
        return "sort_desc"
    if body == "g":
        return "group"
    if len(body) == 1 and body.isdigit():
        return f"digit_{body}"
    return "unknown"


def run_interactive(
    base_commands: List[str],
    initial_filter: str,
    stdin_data: Optional[bytes],
) -> int:
    """
    Run the TUI. Returns subprocess exit code after running a command,
    or 0 when quitting without running.
    """

    def wrapped(stdscr: curses.window) -> int:
        return _tui_main(stdscr, base_commands, initial_filter, stdin_data)

    try:
        return curses.wrapper(wrapped)
    except curses.error:
        print("ihg: terminal does not support curses (need a real TTY)", flush=True)
        return 2


def _tui_main(
    stdscr: curses.window,
    base_commands: List[str],
    initial_filter: str,
    stdin_data: Optional[bytes],
) -> int:
    curses.curs_set(0)
    stdscr.keypad(True)
    try:
        curses.start_color()
    except curses.error:
        pass

    filter_text = initial_filter
    sort_mode: Optional[str] = None
    colon_entry = False
    colon_buffer = ""
    status = ""
    cursor = 0
    scroll = 0

    group_mode = False
    clusters: List[List[str]] = []
    expanded: set[int] = set()
    group_cursor = 0
    group_scroll = 0

    def status_set(msg: str) -> None:
        nonlocal status
        status = msg

    def build_group_rows() -> List[Tuple[str, Optional[str], Optional[int]]]:
        """Rows: (display, command_to_run_or_None, cluster_index_for_header)."""
        rows: List[Tuple[str, Optional[str], Optional[int]]] = []
        for gi, cl in enumerate(clusters):
            head = f"[{len(cl)}] {cl[0][:120]}{'…' if len(cl[0]) > 120 else ''}"
            rows.append((head, None, gi))
            if gi in expanded:
                for m in cl:
                    disp = f"    {m[:120]}{'…' if len(m) > 120 else ''}"
                    rows.append((disp, m, None))
        return rows

    def draw() -> None:
        stdscr.erase()
        h, w = stdscr.getmaxyx()
        if h < 4 or w < 20:
            stdscr.addstr(0, 0, "terminal too small")
            stdscr.refresh()
            return

        footer_h = 2
        main_h = max(1, h - footer_h)

        if group_mode:
            rows = build_group_rows()
            total = len(rows)
            if total == 0:
                stdscr.addstr(0, 0, "(no groups)".ljust(w - 1)[: w - 1])
            else:
                nonlocal group_scroll
                if group_cursor >= group_scroll + main_h:
                    group_scroll = group_cursor - main_h + 1
                if group_cursor < group_scroll:
                    group_scroll = group_cursor
                for i in range(main_h):
                    ri = group_scroll + i
                    if ri >= total:
                        break
                    disp, _, gidx = rows[ri]
                    mark = ">" if ri == group_cursor else " "
                    line = f"{mark} {disp}"
                    if len(line) > w - 1:
                        line = line[: w - 2] + "…"
                    try:
                        stdscr.addstr(i, 0, line)
                    except curses.error:
                        pass
            hint = " Enter: run leaf / toggle group  Esc: back  +/-: expand/collapse "
        else:
            pairs = _visible_pairs(base_commands, filter_text, sort_mode)
            total = len(pairs)
            if total == 0:
                stdscr.addstr(0, 0, "(no matches)".ljust(w - 1)[: w - 1])
            else:
                nonlocal scroll
                if cursor >= scroll + main_h:
                    scroll = cursor - main_h + 1
                if cursor < scroll:
                    scroll = cursor
                for i in range(main_h):
                    ri = scroll + i
                    if ri >= total:
                        break
                    cmd, rec = pairs[ri]
                    label = str(rec) if rec < 10 else " "
                    mark = ">" if ri == cursor else " "
                    body = cmd.replace("\n", " ")
                    if len(body) > w - 8:
                        body = body[: w - 9] + "…"
                    line = f"{mark}{label} {body}"
                    try:
                        stdscr.addstr(i, 0, line.ljust(w - 1)[: w - 1])
                    except curses.error:
                        pass
            so = f" [{sort_mode}]" if sort_mode else ""
            hint = f" ↑/↓ j/k nav  Enter run{so}  :cmds  Esc clear sort/filter "

        foot0 = h - 2
        foot1 = h - 1
        if colon_entry:
            prompt = colon_buffer
        else:
            prompt = f"filter: {filter_text}"
        if len(prompt) > w - 1:
            prompt = prompt[: w - 2] + "…"
        try:
            stdscr.addstr(foot0, 0, prompt.ljust(w - 1)[: w - 1], curses.A_REVERSE)
            msg = status or hint
            if len(msg) > w - 1:
                msg = msg[: w - 2] + "…"
            stdscr.addstr(foot1, 0, msg.ljust(w - 1)[: w - 1])
        except curses.error:
            pass
        stdscr.refresh()

    def run_cmd(cmd: str) -> int:
        piped = stdin_data is not None
        return run_selected(cmd, stdin_data, piped_notice=piped)

    while True:
        draw()
        ch = stdscr.getch()

        if ch == 27:  # Esc
            if group_mode:
                group_mode = False
                status_set("")
                continue
            filter_text = ""
            colon_entry = False
            colon_buffer = ""
            sort_mode = None
            cursor = 0
            scroll = 0
            status_set("")
            continue

        if colon_entry:
            if ch in (curses.KEY_ENTER, 10, 13):
                act = _parse_colon(colon_buffer)
                colon_entry = False
                colon_buffer = ""
                if act == "quit":
                    return 0
                if act == "sort_asc":
                    sort_mode = "asc"
                    status_set("sort A-Z")
                    cursor = 0
                    scroll = 0
                    continue
                if act == "sort_desc":
                    sort_mode = "desc"
                    status_set("sort Z-A")
                    cursor = 0
                    scroll = 0
                    continue
                if act == "group":
                    pairs = _visible_pairs(base_commands, filter_text, sort_mode)
                    cmds = [p[0] for p in pairs]
                    clusters.clear()
                    clusters.extend(_cluster_commands(cmds))
                    expanded.clear()
                    group_mode = True
                    group_cursor = 0
                    group_scroll = 0
                    status_set("similarity groups (:g)")
                    continue
                if act and act.startswith("digit_"):
                    d = int(act[-1])
                    if d < len(base_commands):
                        return run_cmd(base_commands[d])
                    status_set("no command for that index")
                    continue
                status_set("unknown :command")
                continue
            if ch in (curses.KEY_BACKSPACE, 127, 8):
                if len(colon_buffer) > 1:
                    colon_buffer = colon_buffer[:-1]
                else:
                    colon_entry = False
                    colon_buffer = ""
                continue
            if 32 <= ch < 127:
                colon_buffer += chr(ch)
                continue
            continue

        if group_mode:
            rows = build_group_rows()
            total = len(rows)
            if ch in (curses.KEY_UP, ord("k")):
                if group_cursor > 0:
                    group_cursor -= 1
                continue
            if ch in (curses.KEY_DOWN, ord("j")):
                if group_cursor < max(0, total - 1):
                    group_cursor += 1
                continue
            if ch in (ord("+"), ord("=")):
                if total > 0:
                    _, _, gidx = rows[group_cursor]
                    if gidx is not None:
                        expanded.add(gidx)
                continue
            if ch == ord("-"):
                if total > 0:
                    _, _, gidx = rows[group_cursor]
                    if gidx is not None:
                        expanded.discard(gidx)
                continue
            if ch in (curses.KEY_LEFT,):
                if total > 0:
                    _, _, gidx = rows[group_cursor]
                    if gidx is not None:
                        expanded.discard(gidx)
                continue
            if ch in (curses.KEY_RIGHT,):
                if total > 0:
                    _, _, gidx = rows[group_cursor]
                    if gidx is not None:
                        expanded.add(gidx)
                continue
            if ch in (curses.KEY_ENTER, 10, 13):
                if total == 0:
                    continue
                disp, cmd, gidx = rows[group_cursor]
                if cmd:
                    return run_cmd(cmd)
                if gidx is not None:
                    if gidx in expanded:
                        expanded.discard(gidx)
                    else:
                        expanded.add(gidx)
                continue
            continue

        # flat list mode
        pairs = _visible_pairs(base_commands, filter_text, sort_mode)
        total = len(pairs)

        if ch in (curses.KEY_UP, ord("k")):
            if cursor > 0:
                cursor -= 1
            continue
        if ch in (curses.KEY_DOWN, ord("j")):
            if cursor < max(0, total - 1):
                cursor += 1
            continue

        if ch in (curses.KEY_ENTER, 10, 13):
            if total == 0:
                continue
            cmd, _ = pairs[cursor]
            return run_cmd(cmd)

        if ch in (curses.KEY_BACKSPACE, 127, 8):
            filter_text = filter_text[:-1]
            cursor = 0
            scroll = 0
            continue

        if ch == ord(":") and not filter_text:
            colon_entry = True
            colon_buffer = ":"
            continue

        if 32 <= ch < 127:
            filter_text += chr(ch)
            cursor = 0
            scroll = 0
            continue
