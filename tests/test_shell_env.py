"""Tests for shell_subprocess_env."""

import os

import pytest

from ihg.shell_env import shell_subprocess_env


def test_shell_subprocess_env_isolates_histfile(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("HISTFILE", "/home/user/.bash_history")
    monkeypatch.setenv("BASH_ENV", "/etc/corp/bash_env.sh")
    monkeypatch.setenv("PROMPT_COMMAND", "history -a")
    env = shell_subprocess_env()
    assert env["HISTFILE"] == "/dev/null"
    assert env["HISTSIZE"] == "0"
    assert "BASH_ENV" not in env
    assert "PROMPT_COMMAND" not in env


def test_shell_subprocess_env_overrides_histfile_for_zsh_sync() -> None:
    env = shell_subprocess_env({"HISTFILE": "/tmp/zsh_hist"})
    assert env["HISTFILE"] == "/tmp/zsh_hist"
