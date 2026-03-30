"""Static alias parsing and first-token expansion."""

from pathlib import Path

import pytest

from ihg.shell_aliases import (
    expand_command_aliases,
    load_aliases_for_shell,
    _parse_bash_zsh_alias_lines,
)


def test_parse_and_expand_bash_style() -> None:
    d: dict[str, str] = {}
    _parse_bash_zsh_alias_lines(
        "alias ll='ls -l'\n# comment\nalias x=y\n",
        d,
    )
    assert d["ll"] == "ls -l"
    assert d["x"] == "y"
    assert expand_command_aliases("ll foo", d) == "ls -l foo"
    assert expand_command_aliases("ll|wc", d) == "ls -l|wc"


def test_expand_recursive_limited() -> None:
    d = {"a": "b", "b": "c"}
    assert expand_command_aliases("a", d) == "c"


def test_load_from_fake_home(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    monkeypatch.setenv("HOME", str(tmp_path))
    zshrc = tmp_path / ".zshrc"
    zshrc.write_text("alias zz='echo hi'\n", encoding="utf-8")
    m = load_aliases_for_shell("/bin/zsh")
    assert m.get("zz") == "echo hi"


def test_global_alias_skipped() -> None:
    d: dict[str, str] = {}
    _parse_bash_zsh_alias_lines("alias -g G=git\nalias ok=1\n", d)
    assert "G" not in d
    assert d["ok"] == "1"
