"""Clipboard helpers."""

from unittest.mock import MagicMock

import pytest

from ihg.clipboard import copy_text


def test_copy_text_empty_is_success() -> None:
    assert copy_text("") is True


def test_copy_text_pbcopy_macos(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("sys.platform", "darwin")
    mock = MagicMock(return_value=MagicMock(returncode=0))
    monkeypatch.setattr("ihg.clipboard.subprocess.run", mock)
    assert copy_text("hello") is True
    mock.assert_called_once()
    args, kwargs = mock.call_args
    assert args[0] == ["pbcopy"]
    assert kwargs["input"] == b"hello"


def test_copy_text_pbcopy_failure(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("sys.platform", "darwin")
    monkeypatch.setattr(
        "ihg.clipboard.subprocess.run",
        MagicMock(return_value=MagicMock(returncode=1)),
    )
    assert copy_text("x") is False


def test_copy_text_xclip_linux(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("sys.platform", "linux")
    monkeypatch.delenv("WAYLAND_DISPLAY", raising=False)
    monkeypatch.setattr("ihg.clipboard.shutil.which", lambda x: x == "xclip")
    mock = MagicMock(return_value=MagicMock(returncode=0))
    monkeypatch.setattr("ihg.clipboard.subprocess.run", mock)
    assert copy_text("z") is True
    mock.assert_called_once()
    assert mock.call_args[0][0][:2] == ["xclip", "-selection"]
