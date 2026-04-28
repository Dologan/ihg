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


def test_sync_bash_invocation(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", "/bin/bash")
    mock = MagicMock()
    monkeypatch.setattr(hist.subprocess, "run", mock)
    hf = Path("/tmp/ihg_hist_sync_bash")
    hist.sync_shell_history_to_disk_best_effort(hf)
    mock.assert_called_once()
    assert mock.call_args[0][0] == [
        "/bin/bash",
        "--noprofile",
        "--norc",
        "-c",
        "history -a",
    ]
    assert mock.call_args[1]["env"]["HISTFILE"] == str(hf.resolve())


def test_sync_sh_resolved_to_bash_uses_history_a(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """When SHELL is sh but resolves to bash, run history -a on the real bash."""
    fake_sh = tmp_path / "sh"
    fake_bash = tmp_path / "bash"
    fake_sh.symlink_to(fake_bash)
    fake_bash.write_text("#!/bin/bash\nexit 0\n")
    fake_bash.chmod(0o755)
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", str(fake_sh))
    mock = MagicMock()
    monkeypatch.setattr(hist.subprocess, "run", mock)
    hist.sync_shell_history_to_disk_best_effort(tmp_path / "hist")
    mock.assert_called_once()
    assert mock.call_args[0][0][0] == str(fake_bash.resolve())
    assert "history -a" in mock.call_args[0][0]


def test_sync_ignores_subprocess_oserror(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_STARTUP_HISTORY_SYNC", raising=False)
    monkeypatch.setenv("SHELL", "/nonexistent/zsh")
    monkeypatch.setattr(
        hist.subprocess,
        "run",
        MagicMock(side_effect=OSError("no such file")),
    )
    hist.sync_shell_history_to_disk_best_effort()
