"""Tests for dedupe and trivial command filtering."""

from ihg.filters import (
    apply_text_filter,
    build_processed,
    dedupe_newest_first,
    filter_trivial,
    is_trivial_single_token,
)


def test_dedupe_newest_first():
    assert dedupe_newest_first(["a", "b", "a", "c"]) == ["a", "b", "c"]


def test_filter_trivial():
    ig = frozenset({"ls", "pwd"})
    assert is_trivial_single_token("ls", ig) is True
    assert is_trivial_single_token("ls -la", ig) is False
    assert is_trivial_single_token("echo x", ig) is False


def test_build_processed_order():
    newest_first = ["dup", "uniq", "dup", "ls", "git status"]
    cmds, rec = build_processed(newest_first)
    assert "dup" in cmds
    assert "uniq" in cmds
    assert "git status" in cmds
    assert "ls" not in cmds
    assert rec == list(range(len(cmds)))
    assert cmds[0] == "dup"


def test_apply_text_filter():
    cmds = ["git log", "npm test", "git status"]
    rec = [0, 1, 2]
    fc, fr = apply_text_filter(cmds, rec, "git")
    assert fc == ["git log", "git status"]
    assert fr == [0, 2]
