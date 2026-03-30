"""Shell invocation for running history commands."""

import subprocess
import sys
from unittest.mock import MagicMock

import pytest

import ihg.run as ihg_run
from ihg.run import _shell_invocation, run_selected


@pytest.mark.parametrize(
    ("shell", "flag"),
    [
        ("/bin/bash", "-c"),
        ("/opt/homebrew/bin/zsh", "-c"),
        ("/usr/local/bin/zsh", "-c"),
        ("/usr/bin/fish", "-c"),
        ("/bin/sh", "-c"),
        ("/bin/dash", "-c"),
    ],
)
def test_shell_invocation_default_flags(shell: str, flag: str, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_SHELL_FLAGS", raising=False)
    assert _shell_invocation(shell, "x") == [shell, flag, "x"]


def test_shell_invocation_env_override(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("IHG_SHELL_FLAGS", "-foo -bar")
    assert _shell_invocation("/bin/zsh", "c") == ["/bin/zsh", "-foo", "-bar", "c"]


def test_shell_invocation_empty_flags_falls_back_to_c(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("IHG_SHELL_FLAGS", "")
    assert _shell_invocation("/bin/zsh", "c") == ["/bin/zsh", "-c", "c"]


def test_run_selected_prints_command_on_stderr(capsys, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("IHG_NO_PTY", raising=False)
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr(sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(sys.stdout, "isatty", lambda: False)
    mock_run = MagicMock(return_value=subprocess.CompletedProcess([], 0))
    monkeypatch.setattr("ihg.run.subprocess.run", mock_run)
    monkeypatch.setattr("ihg.run._shell_path", lambda: "/bin/sh")
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/bin/sh", "-c", c])

    assert run_selected("echo hello", None, piped_notice=False) == 0
    err = capsys.readouterr().err
    assert "ihg: running:" in err
    assert "echo hello" in err
    mock_run.assert_called_once()
    _, kwargs = mock_run.call_args
    assert kwargs["stdin"] is sys.stdin
    assert kwargs["stdout"] is sys.stdout
    assert kwargs["stderr"] is sys.stderr


def test_run_selected_piped_stdin_message(capsys, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr("ihg.run.subprocess.run", MagicMock(return_value=subprocess.CompletedProcess([], 0)))
    monkeypatch.setattr("ihg.run._shell_path", lambda: "/bin/sh")
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/bin/sh", "-c", c])

    run_selected("grep x", b"line\n", piped_notice=True)
    err = capsys.readouterr().err
    assert "piped stdin" in err
    assert "grep x" in err
