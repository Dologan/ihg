"""Tests for history file parsing."""

from pathlib import Path

from ihg.history import (
    _detect_zsh,
    parse_bash_history,
    parse_history_file,
    parse_zsh_history,
)


def test_parse_zsh_basic():
    text = ": 1320301015:0;cd /tmp\n: 1320301016:0;ls -la\n"
    assert parse_zsh_history(text) == ["cd /tmp", "ls -la"]


def test_parse_zsh_literal_backslash_n_is_not_a_newline():
    # zsh stores a literal backslash-n verbatim; only backslash + real newline continues.
    text = ": 1:0;printf 'a\\nb'\n"
    assert parse_zsh_history(text) == ["printf 'a\\nb'"]


def test_parse_bash_plain():
    text = "echo one\necho two\n"
    assert parse_bash_history(text) == ["echo one", "echo two"]


def test_parse_bash_timestamp():
    text = "#1234567890\necho dated\n#1234567891\necho next\n"
    assert parse_bash_history(text) == ["echo dated", "echo next"]


def test_parse_bash_continuation():
    text = "foo \\\n bar\nnext\n"
    out = parse_bash_history(text)
    assert len(out) == 2
    assert "foo" in out[0]
    assert "bar" in out[0]
    assert out[1] == "next"


def test_detect_zsh():
    z = ": 1:0;a\n: 2:0;b\n"
    b = "ls\npwd\n"
    assert _detect_zsh(z) is True
    assert _detect_zsh(b) is False


def test_parse_history_file_auto(tmp_path: Path):
    z = tmp_path / "z"
    z.write_text(": 1:0;zcmd\n", encoding="utf-8")
    text = z.read_text(encoding="utf-8")
    assert parse_history_file(text) == ["zcmd"]


def test_parse_history_file_bash(tmp_path: Path):
    text = "plain\n"
    assert parse_history_file(text) == ["plain"]
