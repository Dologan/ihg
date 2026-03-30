"""Tests for TUI helpers."""

from ihg.tui import _build_step_groups, _parse_colon, _step_first_words, _visible_pairs


def test_parse_colon():
    assert _parse_colon(":q") == "quit"
    assert _parse_colon(":r") == "reset"
    assert _parse_colon(":e") == "edit"
    assert _parse_colon(":d") == "delete"
    assert _parse_colon(":b") == "backup_default"
    assert _parse_colon(":b /tmp/x") == "backup_to:/tmp/x"
    assert _parse_colon(":s") == "sort_asc"
    assert _parse_colon(":S") == "sort_desc"
    assert _parse_colon(":g") == "group"
    assert _parse_colon(":3") == "digit_3"
    assert _parse_colon(": 7 ") == "digit_7"
    assert _parse_colon(":x") == "unknown"


def test_visible_pairs_sort():
    base = ["b", "a", "c"]
    rec = list(range(3))
    p = _visible_pairs(base, "", "asc")
    texts = [x[0] for x in p]
    assert texts == ["a", "b", "c"]


def test_step_first_words_pipe_and_and():
    assert _step_first_words("ls | grep x") == ["ls", "grep"]
    assert _step_first_words("git pull && npm test") == ["git", "npm"]
    assert _step_first_words("echo ok") == ["echo"]


def test_build_step_groups_repeats_line_under_each_key():
    chained = "ls | grep foo"
    groups = dict(_build_step_groups([chained, "other"]))
    assert groups["ls"] == [chained]
    assert groups["grep"] == [chained]
    assert groups["other"] == ["other"]
