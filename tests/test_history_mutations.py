"""History file append, delete, backup."""

from pathlib import Path

from ihg.filters import build_processed
from ihg.history import (
    append_histfile_line,
    delete_matching_commands,
    load_commands_newest_first,
    parse_history_file,
    write_clean_histfile_backup,
    _parse_bash_blocks,
)


def test_parse_bash_blocks_matches_parse():
    from ihg.history import parse_bash_history

    s = "#1\necho a\n#2\necho b\necho c\n"
    assert [c for c, _ in _parse_bash_blocks(s)] == parse_bash_history(s)


def test_append_and_delete_zsh(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_text(": 1:0;keep\n: 2:0;drop\n", encoding="utf-8")
    append_histfile_line(p, "newcmd", is_zsh=True)
    assert "newcmd" in p.read_text(encoding="utf-8")
    n = delete_matching_commands(p, "drop", is_zsh=True)
    assert n == 1
    text = p.read_text(encoding="utf-8")
    assert "drop" not in text
    assert "keep" in text
    assert ": " in text  # zsh lines preserved


def test_append_and_delete_bash(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_text("one\ntwo\n", encoding="utf-8")
    append_histfile_line(p, "three", is_zsh=False)
    lines = p.read_text(encoding="utf-8").strip().splitlines()
    assert "three" in lines
    n = delete_matching_commands(p, "two", is_zsh=False)
    assert n == 1
    assert "two" not in p.read_text(encoding="utf-8")
    assert "one" in p.read_text(encoding="utf-8")


def test_delete_zsh_preserves_non_entries(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_text("# comment\n: 1:0;x\n\n", encoding="utf-8")
    delete_matching_commands(p, "x", is_zsh=True)
    t = p.read_text(encoding="utf-8")
    assert "# comment" in t


def test_write_clean_backup(tmp_path: Path):
    src = tmp_path / "h"
    src.write_text("ls\ncd /tmp\nls\npwd\n", encoding="utf-8")
    nf = load_commands_newest_first(src)
    dest = tmp_path / "out"
    write_clean_histfile_backup(nf, dest, is_zsh=False)
    body = dest.read_text(encoding="utf-8")
    cmds = parse_history_file(body)
    cleaned, _ = build_processed(nf)
    assert cmds == list(reversed(cleaned))
