package history

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Append adds command as a new history entry without touching existing lines.
// Commands that are blank after trimming are ignored. For zsh the write is
// done under the history lock, in extended form iff the file is empty or
// already extended; for bash a newline is added first if the file does not end
// with one.
func Append(path, command string, f Format) error {
	cmd := strings.TrimRight(strings.ReplaceAll(command, "\r\n", "\n"), "\n")
	if strings.TrimSpace(cmd) == "" {
		return nil
	}
	if f == FormatZsh {
		unlock, err := lockHistfile(path)
		if err != nil {
			return err
		}
		defer unlock()
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		extended := len(data) == 0 || IsZshExtended(data)
		return appendBytes(path, encodeZshEntry(cmd, time.Now().Unix(), extended))
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer file.Close()
	line := []byte(cmd + "\n")
	if st, err := file.Stat(); err != nil {
		return err
	} else if st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, st.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			line = append([]byte("\n"), line...)
		}
	}
	if _, err := file.Write(line); err != nil {
		return err
	}
	return file.Close()
}

func appendBytes(path string, b []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	if _, err := file.Write(b); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Delete removes every entry whose trimmed command equals the trimmed command
// argument exactly, and returns how many were removed. All other bytes of the
// file are preserved. zsh files are rewritten under the history lock; the
// rewrite is atomic.
func Delete(path, command string, f Format) (removed int, err error) {
	want := strings.TrimSpace(command)
	if want == "" {
		return 0, nil
	}
	if f == FormatZsh {
		unlock, err := lockHistfile(path)
		if err != nil {
			return 0, err
		}
		defer unlock()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var kept bytes.Buffer
	for _, e := range ParseEntries(data, f) {
		if e.IsCommand && e.Command == want {
			removed++
			continue
		}
		kept.Write(e.Raw)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := atomicWrite(path, kept.Bytes()); err != nil {
		return 0, err
	}
	return removed, nil
}

// WriteBackup writes the already-cleaned commands (oldest first) to dest,
// creating parent directories. zsh backups use extended entries with
// consecutive timestamps starting at the current time.
func WriteBackup(dest string, oldestFirst []string, f Format) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil {
		return err
	}
	var buf bytes.Buffer
	now := time.Now().Unix()
	for j, c := range oldestFirst {
		if f == FormatZsh {
			buf.Write(encodeZshEntry(c, now+int64(j), true))
		} else {
			buf.WriteString(c + "\n")
		}
	}
	return os.WriteFile(dest, buf.Bytes(), 0o600)
}

// atomicWrite replaces path's contents via temp file + rename so a crash or
// full disk never leaves a truncated history. A symlinked file stays a
// symlink; permission bits are preserved.
func atomicWrite(path string, data []byte) (err error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.ihg")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// Lock parameters, mirroring zsh's lockhistfile. lockTimeout is a variable so
// tests can shorten it.
const (
	lockStale = 10 * time.Second // zsh treats older lock files as abandoned
	lockPoll  = 50 * time.Millisecond
)

var lockTimeout = 2 * time.Second

// lockHistfile takes zsh's history lock: "$HISTFILE.LOCK" created exclusively
// (as zsh's lockhistfile does) plus an fcntl write lock on the file itself for
// HIST_FCNTL_LOCK users. The returned func releases both.
func lockHistfile(path string) (unlock func(), err error) {
	target, rerr := filepath.EvalSymlinks(path)
	if rerr != nil {
		if target, err = filepath.Abs(path); err != nil {
			return nil, err
		}
	}
	lock := target + ".LOCK"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			host, _ := os.Hostname()
			_, werr := fmt.Fprintf(f, "%d %s\n", os.Getpid(), host)
			f.Close()
			if werr != nil {
				os.Remove(lock)
				return nil, werr
			}
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		st, serr := os.Stat(lock)
		if errors.Is(serr, fs.ErrNotExist) {
			continue
		} else if serr != nil {
			return nil, serr
		}
		if time.Since(st.ModTime()) > lockStale {
			if rerr := os.Remove(lock); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return nil, rerr
			}
			continue
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("history file is locked (%s)", lock)
		}
		time.Sleep(lockPoll)
	}

	hf, err := os.OpenFile(target, os.O_RDWR, 0)
	if err != nil {
		os.Remove(lock)
		return nil, err
	}
	for {
		busy, ferr := fcntlTryLock(hf)
		if !busy {
			_ = ferr // other errors (e.g. no fcntl locks on this filesystem): the .LOCK file still applies
			break
		}
		if !time.Now().Before(deadline) {
			hf.Close()
			os.Remove(lock)
			return nil, fmt.Errorf("history file is locked (fcntl, %s)", target)
		}
		time.Sleep(lockPoll)
	}
	return func() {
		hf.Close() // also drops the fcntl lock
		os.Remove(lock)
	}, nil
}
