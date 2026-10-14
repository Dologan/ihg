"""zsh on-disk history format: metafication, continuations, byte-exact rewrites."""

from __future__ import annotations

import os
import shutil
import subprocess
import time
from pathlib import Path

import pytest

from ihg.history import (
    _detect_zsh,
    _zsh_encode_entry,
    histfile_is_zsh,
    append_histfile_line,
    delete_matching_commands,
    load_commands_chronological,
    parse_zsh_history,
    zsh_metafy,
    zsh_unmetafy,
)

# "→" is E2 86 92; zsh stores 0x86/0x92 as 0x83 0xa6 / 0x83 0xb2.
ARROW_ON_DISK = b"\xe2\x83\xa6\x83\xb2"

TRICKY = [
    "printf 'a\\nb'",
    "echo café → ✓",
    "for i in 1 2; do\n  echo $i\ndone",
    "echo trailing\\",
    "echo bs space\\ ",
    "line\\\nnext",
    "echo \udc83\udca2 raw-bytes",  # undecodable 0x83 0xa2 (surrogate escapes)
]


def test_metafy_roundtrip_all_bytes():
    data = bytes(range(256))
    assert zsh_unmetafy(zsh_metafy(data)) == data
    assert zsh_metafy("→".encode()) == ARROW_ON_DISK


def test_parse_metafied_utf8():
    assert parse_zsh_history(b": 1:0;echo " + ARROW_ON_DISK + b"\n") == ["echo →"]


def test_parse_multiline_continuation():
    data = b": 1:0;for i in 1 2; do\\\n  echo $i\\\ndone\n: 2:0;next\n"
    assert parse_zsh_history(data) == ["for i in 1 2; do\n  echo $i\ndone", "next"]


def test_trailing_backslash_guard_space_is_removed():
    # zsh writes a command ending in "\" as "\ " so it is not read as a continuation.
    assert parse_zsh_history(b": 1:0;echo x\\ \n: 2:0;y\n") == ["echo x\\", "y"]


@pytest.mark.parametrize("cmd", TRICKY)
def test_encode_then_parse_roundtrip(cmd: str):
    assert parse_zsh_history(_zsh_encode_entry(cmd, 1)) == [cmd.strip()]


def test_delete_keeps_other_entries_byte_exact(tmp_path: Path):
    keep = (
        b": 1:0;echo caf\xc3\xa9\n"
        b": 2:0;echo " + ARROW_ON_DISK + b"\n"
        b": 3:0;multi\\\nline\n"
        b"garbage line\n"
    )
    p = tmp_path / "hist"
    p.write_bytes(keep + b": 4:0;git status\n")
    assert delete_matching_commands(p, "git status", is_zsh=True) == 1
    assert p.read_bytes() == keep


def test_delete_multiline_entry_removes_all_its_lines(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_bytes(b": 1:0;a\n: 2:0;multi\\\nline\n: 3:0;b\n")
    assert delete_matching_commands(p, "multi\nline", is_zsh=True) == 1
    assert p.read_bytes() == b": 1:0;a\n: 3:0;b\n"


def test_delete_preserves_symlink_and_mode(tmp_path: Path):
    real = tmp_path / "real_hist"
    real.write_bytes(b": 1:0;a\n: 2:0;b\n")
    real.chmod(0o600)
    link = tmp_path / "link_hist"
    link.symlink_to(real)
    assert delete_matching_commands(link, "a", is_zsh=True) == 1
    assert link.is_symlink()
    assert real.read_bytes() == b": 2:0;b\n"
    assert (real.stat().st_mode & 0o777) == 0o600
    assert not any(p.name.endswith(".ihg") for p in tmp_path.iterdir())


def test_delete_waits_for_fresh_zsh_lock(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    import ihg.history as hist

    monkeypatch.setattr(hist, "_ZSH_LOCK_TIMEOUT_SECS", 0.1)
    p = tmp_path / "hist"
    p.write_bytes(b": 1:0;a\n")
    (tmp_path / "hist.LOCK").write_text("1 host\n")
    with pytest.raises(OSError, match="locked"):
        delete_matching_commands(p, "a", is_zsh=True)
    assert p.read_bytes() == b": 1:0;a\n"


def test_stale_zsh_lock_is_taken_over(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_bytes(b": 1:0;a\n: 2:0;b\n")
    lock = tmp_path / "hist.LOCK"
    lock.write_text("1 host\n")
    old = time.time() - 60
    os.utime(lock, (old, old))
    assert delete_matching_commands(p, "a", is_zsh=True) == 1
    assert not lock.exists()


def test_append_zsh_does_not_double_backslashes(tmp_path: Path):
    p = tmp_path / "hist"
    p.write_bytes(b": 1:0;a\n")
    append_histfile_line(p, "printf 'x\\ny'", is_zsh=True)
    assert p.read_bytes().endswith(b":0;printf 'x\\ny'\n")
    assert load_commands_chronological(p) == ["a", "printf 'x\\ny'"]


def test_plain_zsh_file_multiline_and_metafied(tmp_path: Path):
    # No EXTENDED_HISTORY: no headers, but still metafied with continuations.
    p = tmp_path / ".zsh_history"
    p.write_bytes(b"echo one\nfor x in a; do\\\n  echo " + ARROW_ON_DISK + b"\\\ndone\n")
    assert histfile_is_zsh(p)
    assert load_commands_chronological(p) == ["echo one", "for x in a; do\n  echo →\ndone"]


def test_plain_zsh_append_stays_plain(tmp_path: Path):
    p = tmp_path / ".zsh_history"
    p.write_bytes(b"echo one\n")
    append_histfile_line(p, "echo two", is_zsh=True)
    assert p.read_bytes() == b"echo one\necho two\n"


@pytest.mark.parametrize(
    "name, body, shell, expect",
    [
        ("hist", b": 1:0;a\n", "/bin/bash", True),  # extended header wins
        ("hist", b"#1700000000\na\n", "/bin/zsh", False),  # bash timestamps win
        (".zsh_history", b"a\n", "/bin/bash", True),
        (".bash_history", b"a\n", "/bin/zsh", False),
        ("hist", b"a\n", "/bin/zsh", True),  # fall back to the shell
        ("hist", b"a\n", "/bin/bash", False),
    ],
)
def test_histfile_format_detection(tmp_path, monkeypatch, name, body, shell, expect):
    monkeypatch.delenv("IHG_SHELL", raising=False)
    monkeypatch.setenv("SHELL", shell)
    p = tmp_path / name
    p.write_bytes(body)
    assert histfile_is_zsh(p) is expect


def test_bash_delete_byte_exact_with_invalid_utf8_and_blank_lines(tmp_path: Path):
    keep = b"echo \xff\xfe legacy\n\n#1700000000\necho dated\n"
    p = tmp_path / "hist"
    p.write_bytes(keep + b"rm me\n")
    assert delete_matching_commands(p, "rm me", is_zsh=False) == 1
    assert p.read_bytes() == keep


ZSH = shutil.which("zsh")


@pytest.mark.skipif(ZSH is None, reason="zsh not installed")
@pytest.mark.parametrize("extended", [True, False], ids=["extended", "plain"])
def test_real_zsh_agrees_both_directions(tmp_path: Path, extended: bool):
    """zsh writes -> ihg parses, and ihg writes -> zsh reads, for tricky commands."""
    enc = [c.encode("utf-8", "surrogateescape") for c in TRICKY]
    cmds_file = tmp_path / "cmds"
    cmds_file.write_bytes(b"\0".join(enc) + b"\0")
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(tmp_path)}

    # 1. zsh writes, ihg parses.
    zfile = tmp_path / ".zsh_history"
    opt = "setopt extended_history; " if extended else ""
    subprocess.run(
        [ZSH, "-f", "-i", "-c",
         opt + 'HISTSIZE=100 SAVEHIST=100 HISTFILE="$1"; '
         "while IFS= read -r -d '' c; do print -rs -- \"$c\"; done < \"$2\"; fc -W",
         "zsh", str(zfile), str(cmds_file)],
        env=env, check=True, capture_output=True,
    )
    assert _detect_zsh(zfile.read_bytes()) is extended
    assert load_commands_chronological(zfile) == [c.strip() for c in TRICKY]

    # 2. ihg writes, zsh reads.
    ifile = tmp_path / "ihg_written"
    ts = 1 if extended else None
    ifile.write_bytes(b"".join(_zsh_encode_entry(c, ts) for c in TRICKY))
    out = subprocess.run(
        [ZSH, "-f", "-i", "-c",
         "zmodload zsh/parameter; HISTSIZE=100; fc -R \"$1\"; "
         "for ((i = 1; i <= HISTCMD; i++)); do "
         "(( ${+history[$i]} )) && print -rn -- \"${history[$i]}\"$'\\0'; done",
         "zsh", str(ifile)],
        env=env, check=True, capture_output=True,
    ).stdout
    got = [b.decode("utf-8", "surrogateescape") for b in out.split(b"\0")[:-1]]
    assert got == TRICKY
