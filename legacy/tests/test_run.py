"""Shell invocation for running history commands."""

import subprocess
import sys
from pathlib import Path
from unittest.mock import MagicMock

import pytest

import ihg.run as ihg_run
from ihg.run import (
    _shell_invocation,
    clear_parent_shell_eval_file,
    expand_command_for_shell,
    run_selected,
)


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


def test_expand_command_for_shell(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {"ll": "ls -l"})
    assert expand_command_for_shell("ll /tmp") == "ls -l /tmp"


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


def test_zsh_prepends_nonomatch_for_glob_commands(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr(ihg_run, "_shell_path", lambda: "/usr/bin/zsh")
    monkeypatch.delenv("IHG_NO_ZSH_NONOMATCH", raising=False)
    monkeypatch.setattr(sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(sys.stdout, "isatty", lambda: False)
    captured: list[list[str]] = []

    def fake_run(*a: object, **k: object) -> subprocess.CompletedProcess[object]:
        captured.append(a[0] if a else [])
        return subprocess.CompletedProcess([], 0)

    monkeypatch.setattr("ihg.run.subprocess.run", fake_run)
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/usr/bin/zsh", "-c", c])
    ihg_run.run_selected("ls -l ~/*.sh", None, piped_notice=False)
    assert captured
    inner = captured[0][-1]
    assert inner.startswith("setopt nonomatch")
    assert "ls -l ~/*.sh" in inner


def test_simple_cd_writes_parent_eval_file_no_subprocess(
    capsys: pytest.CaptureFixture[str],
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    monkeypatch.setenv("XDG_STATE_HOME", str(tmp_path))
    monkeypatch.delenv("IHG_NO_PARENT_SHELL_EVAL", raising=False)
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr(sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(sys.stdout, "isatty", lambda: False)
    mock_run = MagicMock(return_value=subprocess.CompletedProcess([], 0))
    monkeypatch.setattr("ihg.run.subprocess.run", mock_run)

    assert run_selected("cd /tmp", None, piped_notice=False) == 0
    mock_run.assert_not_called()
    p = tmp_path / "ihg" / "last_eval.sh"
    assert p.read_text(encoding="utf-8") == "cd /tmp\n"
    err = capsys.readouterr().err
    assert "cd /tmp" in err


def test_cd_with_compound_runs_subprocess(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    monkeypatch.setenv("XDG_STATE_HOME", str(tmp_path))
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr(sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(sys.stdout, "isatty", lambda: False)
    mock_run = MagicMock(return_value=subprocess.CompletedProcess([], 0))
    monkeypatch.setattr("ihg.run.subprocess.run", mock_run)
    monkeypatch.setattr("ihg.run._shell_path", lambda: "/bin/sh")
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/bin/sh", "-c", c])

    assert run_selected("cd /tmp && ls", None, piped_notice=False) == 0
    mock_run.assert_called_once()
    assert not (tmp_path / "ihg" / "last_eval.sh").exists()


def test_ihg_no_parent_shell_eval_forces_subprocess(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    monkeypatch.setenv("XDG_STATE_HOME", str(tmp_path))
    monkeypatch.setenv("IHG_NO_PARENT_SHELL_EVAL", "1")
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr(sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(sys.stdout, "isatty", lambda: False)
    mock_run = MagicMock(return_value=subprocess.CompletedProcess([], 0))
    monkeypatch.setattr("ihg.run.subprocess.run", mock_run)
    monkeypatch.setattr("ihg.run._shell_path", lambda: "/bin/sh")
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/bin/sh", "-c", c])

    assert run_selected("cd /var", None, piped_notice=False) == 0
    mock_run.assert_called_once()


def test_clear_parent_shell_eval_file_truncates(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    monkeypatch.setenv("XDG_STATE_HOME", str(tmp_path))
    p = tmp_path / "ihg" / "last_eval.sh"
    p.parent.mkdir(parents=True)
    p.write_text("cd /old\n", encoding="utf-8")
    clear_parent_shell_eval_file()
    assert p.read_text(encoding="utf-8") == ""


def test_run_selected_piped_stdin_message(capsys, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(ihg_run, "_SHELL_ALIASES", {})
    monkeypatch.setattr("ihg.run.subprocess.run", MagicMock(return_value=subprocess.CompletedProcess([], 0)))
    monkeypatch.setattr("ihg.run._shell_path", lambda: "/bin/sh")
    monkeypatch.setattr("ihg.run._shell_invocation", lambda _s, c: ["/bin/sh", "-c", c])

    run_selected("grep x", b"line\n", piped_notice=True)
    err = capsys.readouterr().err
    assert "piped stdin" in err
    assert "grep x" in err
