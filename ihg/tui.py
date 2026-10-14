"""curses UI for browsing and running history entries."""

from __future__ import annotations

import curses
import re
import sys
from datetime import datetime
from pathlib import Path
from typing import List, Optional, Tuple

from ihg.filters import apply_text_filter, build_processed
from ihg.history import (
    append_histfile_line,
    delete_matching_commands,
    load_commands_newest_first,
    write_clean_histfile_backup,
)
from ihg.clipboard import copy_text
from ihg.run import expand_command_for_shell, run_selected

Pair = Tuple[str, int]  # (command, recency_rank)

_HELP_LINES: Tuple[str, ...] = (
    "ihg — quick help",
    "",
    "Move        ↑/↓",
    "Run         Enter (footer [COPY] = copy alias line, then quit)",
    "Edit        ← or :e (row highlighted; differs from [COPY] color)",
    "Clipboard   → copy mode   :c = copy once & quit   Esc clears filter/copy",
    "Copy fail   expanded line printed on stdout for manual copy (e.g. SSH)",
    "Filter      type text; empty filter + : for colon commands",
    "Colon       :q quit   :r reset   :s :S sort   :g groups   :h this help",
    "            :e edit   :d delete   :b backup   :0–:9 pick",
    "Groups      Space on a header toggles expand/collapse; +/- also",
    "",
    "Esc         closes help (here) or clears filter / leaves group view",
)


def _printable(s: str) -> str:
    """Make undecodable history bytes (surrogate escapes) safe for curses output."""
    return s.encode("utf-8", errors="surrogateescape").decode("utf-8", errors="replace")


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


_STEP_SPLIT = re.compile(r"\s*(?:&&|\|)\s*")


def _step_first_words(cmd: str) -> List[str]:
    """First token of each &&- or |-separated step; order preserved, duplicates dropped."""
    s = cmd.strip()
    if not s:
        return []
    keys: List[str] = []
    seen: set[str] = set()
    for part in _STEP_SPLIT.split(s):
        part = part.strip()
        if not part:
            continue
        word = part.split()[0]
        if word not in seen:
            seen.add(word)
            keys.append(word)
    return keys


def _build_step_groups(commands: List[str], limit: int = 500) -> List[Tuple[str, List[str]]]:
    """
    (first word of a step, full lines that include that step).
    A line is listed under every group for each distinct step-initial word it contains.
    """
    items = commands[:limit]
    bucket: dict[str, List[str]] = {}
    for cmd in items:
        for key in _step_first_words(cmd):
            bucket.setdefault(key, []).append(cmd)
    return sorted(bucket.items(), key=lambda x: x[0].casefold())


def _parse_colon(buf: str) -> Optional[str]:
    """
    Return action name: quit, reset, sort_asc, sort_desc, group, edit, help,
    copy_clipboard, delete, backup_default, backup_to:PATH, digit_N, unknown.
    buf must include leading ':'.
    """
    if not buf.startswith(":"):
        return None
    body = buf[1:].strip()
    if body == "q":
        return "quit"
    if body == "r":
        return "reset"
    if body == "s":
        return "sort_asc"
    if body == "S":
        return "sort_desc"
    if body == "g":
        return "group"
    if body == "e":
        return "edit"
    if body == "h":
        return "help"
    if body == "c":
        return "copy_clipboard"
    if body == "d":
        return "delete"
    if body == "b":
        return "backup_default"
    if body.startswith("b ") and body[2:].strip():
        return f"backup_to:{body[2:].strip()}"
    if len(body) == 1 and body.isdigit():
        return f"digit_{body}"
    return "unknown"


def run_interactive(
    base_commands: List[str],
    initial_filter: str,
    stdin_data: Optional[bytes],
    histfile_path: Path,
    histfile_zsh: bool,
) -> int:
    """
    Run the TUI. Returns subprocess exit code after running a command,
    0 when quitting without running, or 0/1 after copying to the clipboard.
    """
    stdscr: Optional[curses.window] = None
    try:
        stdscr = curses.initscr()
        curses.noecho()
        curses.cbreak()
        stdscr.keypad(True)
        try:
            curses.start_color()
        except curses.error:
            pass
        return _tui_main(
            stdscr,
            base_commands,
            initial_filter,
            stdin_data,
            histfile_path,
            histfile_zsh,
        )
    except curses.error:
        print("ihg: terminal does not support curses (need a real TTY)", flush=True)
        return 2
    finally:
        if stdscr is not None:
            try:
                stdscr.keypad(False)
            except curses.error:
                pass
            try:
                curses.echo()
                curses.nocbreak()
            except curses.error:
                pass
            try:
                curses.endwin()
            except curses.error:
                pass


def _tui_main(
    stdscr: curses.window,
    base_commands: List[str],
    initial_filter: str,
    stdin_data: Optional[bytes],
    histfile_path: Path,
    histfile_zsh: bool,
) -> int:
    curses.curs_set(0)
    stdscr.keypad(True)
    try:
        curses.start_color()
    except curses.error:
        pass

    norm_attr = 0
    sel_attr = curses.A_REVERSE
    clip_sel_attr = curses.A_STANDOUT | curses.A_BOLD
    edit_sel_attr = curses.A_UNDERLINE | curses.A_BOLD
    if curses.has_colors():
        try:
            curses.use_default_colors()
            curses.init_pair(1, curses.COLOR_CYAN, -1)
            sel_attr = curses.color_pair(1) | curses.A_BOLD
            curses.init_pair(2, curses.COLOR_BLACK, curses.COLOR_BLUE)
            clip_sel_attr = curses.color_pair(2) | curses.A_BOLD
            curses.init_pair(3, curses.COLOR_BLACK, curses.COLOR_MAGENTA)
            edit_sel_attr = curses.color_pair(3) | curses.A_BOLD
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
    group_sections: List[Tuple[str, List[str]]] = []
    expanded: set[int] = set()
    group_cursor = 0
    group_scroll = 0

    edit_mode = False
    edit_buffer = ""
    edit_curs = 0
    clipboard_mode = False
    help_mode = False

    try:
        _hst = histfile_path.stat()
        hist_track: Tuple[int, int] = (_hst.st_mtime_ns, _hst.st_size)
    except OSError:
        hist_track = (0, 0)

    def status_set(msg: str) -> None:
        nonlocal status
        status = msg

    def open_edit_from_selection() -> None:
        nonlocal edit_mode, edit_buffer, edit_curs, group_mode
        cmd = selected_command()
        if cmd is None:
            status_set("select a command line to edit")
            return
        edit_mode = True
        edit_buffer = cmd
        edit_curs = len(edit_buffer)
        group_mode = False
        status_set("edit: Enter run or copy  Esc cancel")

    def reload_from_disk() -> None:
        nonlocal cursor, scroll, group_mode, group_sections, expanded
        nonlocal group_cursor, group_scroll, hist_track, clipboard_mode, help_mode
        nonlocal edit_mode, edit_buffer, edit_curs
        nf = load_commands_newest_first(histfile_path)
        b, _ = build_processed(nf)
        base_commands.clear()
        base_commands.extend(b)
        group_mode = False
        group_sections.clear()
        expanded.clear()
        group_cursor = 0
        group_scroll = 0
        clipboard_mode = False
        help_mode = False
        edit_mode = False
        edit_buffer = ""
        edit_curs = 0
        pairs = _visible_pairs(base_commands, filter_text, sort_mode)
        if not pairs:
            cursor = 0
            scroll = 0
        else:
            cursor = min(cursor, len(pairs) - 1)
            scroll = min(scroll, max(0, cursor))
        try:
            st = histfile_path.stat()
            hist_track = (st.st_mtime_ns, st.st_size)
        except OSError:
            pass

    def reload_if_histfile_changed() -> None:
        try:
            st = histfile_path.stat()
        except OSError:
            return
        tok = (st.st_mtime_ns, st.st_size)
        if tok == hist_track:
            return
        reload_from_disk()

    def selected_command() -> Optional[str]:
        if group_mode:
            rows = build_group_rows()
            if not rows or group_cursor >= len(rows):
                return None
            _, cmd, _ = rows[group_cursor]
            return cmd
        pairs = _visible_pairs(base_commands, filter_text, sort_mode)
        if not pairs or cursor >= len(pairs):
            return None
        return pairs[cursor][0]

    def build_group_rows() -> List[Tuple[str, Optional[str], Optional[int]]]:
        """Rows: (display, command_to_run_or_None, section_index_for_header)."""
        rows: List[Tuple[str, Optional[str], Optional[int]]] = []
        for gi, (key, members) in enumerate(group_sections):
            kdisp = _printable(key[:120]) + ("…" if len(key) > 120 else "")
            head = f"[{len(members)}] {kdisp}"
            rows.append((head, None, gi))
            if gi in expanded:
                for m in members:
                    disp = f"    {_printable(m[:120])}{'…' if len(m) > 120 else ''}"
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

        if help_mode:
            for ri, line in enumerate(_HELP_LINES):
                if ri >= h - 1:
                    break
                clipped = line[: w - 1].ljust(w - 1)[: w - 1]
                try:
                    stdscr.addstr(ri, 0, clipped)
                except curses.error:
                    pass
            foot = " Esc — close help "
            try:
                stdscr.addstr(
                    h - 1,
                    0,
                    foot.ljust(w - 1)[: w - 1],
                    curses.A_REVERSE,
                )
            except curses.error:
                pass
            stdscr.refresh()
            return

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
                    disp, _, _ = rows[ri]
                    mark = ">" if ri == group_cursor else " "
                    line = f"{mark} {disp}"
                    if len(line) > w - 1:
                        line = line[: w - 2] + "…"
                    line = line.ljust(w - 1)[: w - 1]
                    if ri == group_cursor and clipboard_mode:
                        row_attr = clip_sel_attr
                    elif ri == group_cursor:
                        row_attr = sel_attr
                    else:
                        row_attr = norm_attr
                    try:
                        stdscr.addstr(i, 0, line, row_attr)
                    except curses.error:
                        pass
            cm = " [COPY]" if clipboard_mode else ""
            hint = (
                f" Enter{cm}  Space hdr  +/- expand  ←edit →copy  "
                ":e/:d/:b/:c/:h  :r  Esc back "
            )
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
                    body = _printable(cmd).replace("\n", " ")
                    if len(body) > w - 8:
                        body = body[: w - 9] + "…"
                    line = f"{mark}{label} {body}".ljust(w - 1)[: w - 1]
                    if edit_mode and ri == cursor:
                        row_attr = edit_sel_attr
                    elif ri == cursor and clipboard_mode:
                        row_attr = clip_sel_attr
                    elif ri == cursor:
                        row_attr = sel_attr
                    else:
                        row_attr = norm_attr
                    try:
                        stdscr.addstr(i, 0, line, row_attr)
                    except curses.error:
                        pass
            so = f" [{sort_mode}]" if sort_mode else ""
            cm = " [COPY]" if clipboard_mode else ""
            hint = (
                f" ↑/↓  Enter{cm}{so}  ←edit →copy  "
                ":e/:d/:b/:c/:h  :r  Esc clear "
            )

        foot0 = h - 2
        foot1 = h - 1
        if edit_mode:
            disp = _printable(edit_buffer).replace("\n", " ")
            prefix = "edit: "
            wlim = max(1, w - 1)
            avail = max(4, wlim - len(prefix))
            dc = len(edit_buffer[:edit_curs].replace("\n", " "))
            dc = min(dc, len(disp))
            if len(disp) <= avail:
                chunk = disp
                mark = dc
            else:
                start = max(0, min(dc - avail // 2, len(disp) - avail))
                chunk = disp[start : start + avail]
                mark = min(max(dc - start, 0), len(chunk))
            mid = chunk[:mark] + "|" + chunk[mark:]
            prompt = (prefix + mid)[:wlim]
            if len(prefix + mid) > wlim:
                prompt = (prefix + mid)[: wlim - 1] + "…"
        elif colon_entry:
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
        # Release the TTY for the child; our init/finally runs only after we return.
        curses.endwin()
        return run_selected(cmd, stdin_data, piped_notice=piped)

    def copy_selected_and_exit(cmd: str) -> int:
        curses.endwin()
        expanded = expand_command_for_shell(cmd)
        ok = copy_text(expanded)
        if ok:
            print(
                f"ihg: copied to clipboard ({len(expanded)} chars)",
                file=sys.stderr,
            )
        else:
            print(
                "ihg: clipboard unavailable (try pbcopy, wl-copy, xclip, or xsel); "
                "command printed below for manual copy",
                file=sys.stderr,
            )
            sys.stdout.flush()
            sys.stdout.buffer.write(
                expanded.encode("utf-8", errors="surrogateescape") + b"\n"
            )
            sys.stdout.buffer.flush()
        return 0 if ok else 1

    while True:
        reload_if_histfile_changed()
        draw()
        ch = stdscr.getch()

        if help_mode:
            if ch == 27:
                help_mode = False
                status_set("")
            continue

        if ch == 27:  # Esc
            if edit_mode:
                edit_mode = False
                status_set("")
                continue
            if group_mode:
                group_mode = False
                help_mode = False
                status_set("")
                continue
            clipboard_mode = False
            help_mode = False
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
                if act == "reset":
                    sort_mode = None
                    group_mode = False
                    group_sections.clear()
                    expanded.clear()
                    group_cursor = 0
                    group_scroll = 0
                    cursor = 0
                    scroll = 0
                    clipboard_mode = False
                    help_mode = False
                    status_set("")
                    continue
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
                    group_sections.clear()
                    group_sections.extend(_build_step_groups(cmds))
                    expanded.clear()
                    group_mode = True
                    group_cursor = 0
                    group_scroll = 0
                    status_set("groups by command step (:g)")
                    continue
                if act == "edit":
                    open_edit_from_selection()
                    continue
                if act == "help":
                    help_mode = True
                    status_set("")
                    continue
                if act == "copy_clipboard":
                    cmd = selected_command()
                    if cmd is None:
                        status_set("select a command to copy")
                        continue
                    return copy_selected_and_exit(cmd)
                if act == "delete":
                    cmd = selected_command()
                    if cmd is None:
                        status_set("select a command line to delete")
                        continue
                    try:
                        n = delete_matching_commands(
                            histfile_path, cmd, is_zsh=histfile_zsh
                        )
                    except OSError as exc:
                        status_set(f"delete failed: {exc}")
                        continue
                    reload_from_disk()
                    group_mode = False
                    status_set(
                        f"removed {n} matching entries" if n else "no matching entries"
                    )
                    continue
                if act == "backup_default":
                    dest = histfile_path.parent / (
                        f"{histfile_path.name}.ihg-clean."
                        f"{datetime.now():%Y%m%d-%H%M%S}"
                    )
                    try:
                        cmds_nf = load_commands_newest_first(histfile_path)
                        write_clean_histfile_backup(
                            cmds_nf,
                            dest,
                            is_zsh=histfile_zsh,
                        )
                        status_set(f"backup → {dest}")
                    except OSError as exc:
                        status_set(f"backup failed: {exc}")
                    continue
                if act.startswith("backup_to:"):
                    dest = Path(act[10:]).expanduser()
                    try:
                        cmds_nf = load_commands_newest_first(histfile_path)
                        write_clean_histfile_backup(
                            cmds_nf,
                            dest,
                            is_zsh=histfile_zsh,
                        )
                        status_set(f"backup → {dest}")
                    except OSError as exc:
                        status_set(f"backup failed: {exc}")
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

        if edit_mode:
            if ch in (curses.KEY_ENTER, 10, 13):
                cmd = edit_buffer.strip()
                if not cmd:
                    edit_mode = False
                    status_set("empty command")
                    continue
                if clipboard_mode:
                    return copy_selected_and_exit(cmd)
                try:
                    append_histfile_line(
                        histfile_path, cmd, is_zsh=histfile_zsh
                    )
                except OSError as exc:
                    status_set(f"append history failed: {exc}")
                    continue
                edit_mode = False
                return run_cmd(cmd)
            if ch in (curses.KEY_BACKSPACE, 127, 8):
                if edit_curs > 0:
                    edit_buffer = (
                        edit_buffer[: edit_curs - 1] + edit_buffer[edit_curs :]
                    )
                    edit_curs -= 1
                continue
            if ch in (curses.KEY_LEFT,):
                edit_curs = max(0, edit_curs - 1)
                continue
            if ch in (curses.KEY_RIGHT,):
                edit_curs = min(len(edit_buffer), edit_curs + 1)
                continue
            if ch in (curses.KEY_HOME,):
                edit_curs = 0
                continue
            if ch in (curses.KEY_END,):
                edit_curs = len(edit_buffer)
                continue
            if 32 <= ch < 127:
                c = chr(ch)
                edit_buffer = (
                    edit_buffer[:edit_curs] + c + edit_buffer[edit_curs:]
                )
                edit_curs += 1
                continue
            continue

        if group_mode:
            if ch == ord(":"):
                colon_entry = True
                colon_buffer = ":"
                continue
            rows = build_group_rows()
            total = len(rows)
            if ch in (curses.KEY_UP,):
                if group_cursor > 0:
                    group_cursor -= 1
                continue
            if ch in (curses.KEY_DOWN,):
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
            if ch == ord(" "):
                if total == 0:
                    continue
                _, cmd, gidx = rows[group_cursor]
                if cmd is None and gidx is not None:
                    if gidx in expanded:
                        expanded.discard(gidx)
                    else:
                        expanded.add(gidx)
                continue
            if ch in (curses.KEY_LEFT,):
                open_edit_from_selection()
                continue
            if ch in (curses.KEY_RIGHT,):
                clipboard_mode = True
                continue
            if ch in (curses.KEY_ENTER, 10, 13):
                if total == 0:
                    continue
                disp, cmd, gidx = rows[group_cursor]
                if cmd:
                    if clipboard_mode:
                        return copy_selected_and_exit(cmd)
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

        if ch in (curses.KEY_UP,):
            if cursor > 0:
                cursor -= 1
            continue
        if ch in (curses.KEY_DOWN,):
            if cursor < max(0, total - 1):
                cursor += 1
            continue

        if ch in (curses.KEY_LEFT,):
            open_edit_from_selection()
            continue
        if ch in (curses.KEY_RIGHT,):
            clipboard_mode = True
            continue

        if ch in (curses.KEY_ENTER, 10, 13):
            if total == 0:
                continue
            cmd, _ = pairs[cursor]
            if clipboard_mode:
                return copy_selected_and_exit(cmd)
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
