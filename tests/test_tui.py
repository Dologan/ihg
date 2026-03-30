"""Tests for TUI helpers."""

from ihg.tui import _parse_colon, _visible_pairs


def test_parse_colon():
    assert _parse_colon(":q") == "quit"
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
