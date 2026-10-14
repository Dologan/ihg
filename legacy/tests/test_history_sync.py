"""Startup history sync before loading HISTFILE."""

from pathlib import Path
from unittest.mock import MagicMock

import pytest

import ihg.history as hist


def test_sync_skipped_when_env_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("IHG_NO_STARTUP_HISTORY_SYNC", "1")
    mock = MagicMock()
    monkeypatch.setattr(hist.subprocess, "run", mock)
    hist.sync_shell_history_to_disk_best_effort()
    mock.assert_not_called()


def test_sync_zsh_invocation(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", "/bin/zsh")
    mock = MagicMock()
    monkeypatch.setattr(hist.subprocess, "run", mock)
    hf = Path("/tmp/ihg_hist_sync_zsh")
    hist.sync_shell_history_to_disk_best_effort(hf)
    mock.assert_called_once()
    args, kwargs = mock.call_args
    assert args[0] == ["/bin/zsh", "-f", "-c", "fc -AI"]
    assert kwargs["stdin"] == hist.subprocess.DEVNULL
    assert kwargs["timeout"] == 5
    assert kwargs["env"]["HISTFILE"] == str(hf.resolve())
    assert kwargs["env"]["HISTFILE"] != "/dev/null"


def test_sync_bash_does_not_invoke_subprocess(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", "/bin/bash")
    mock = MagicMock()
    monkeypatch.setattr(hist.subprocess, "run", mock)
    hf = Path("/tmp/ihg_hist_sync_bash")
    hist.sync_shell_history_to_disk_best_effort(hf)
    mock.assert_not_called()


def test_sync_ignores_subprocess_oserror(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", "/nonexistent/zsh")
    monkeypatch.setattr(
        hist.subprocess,
        "run",
        MagicMock(side_effect=OSError("no such file")),
    )
    hist.sync_shell_history_to_disk_best_effort()
