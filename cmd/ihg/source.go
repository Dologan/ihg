package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Dologan/ihg/internal/clipboard"
	"github.com/Dologan/ihg/internal/filters"
	"github.com/Dologan/ihg/internal/history"
)

// source is the history file behind a picker session. It supplies the
// commands and implements the UI's delete, backup and reload callbacks.
type source struct {
	path   string
	shell  string
	ignore map[string]bool
	format history.Format
	stamp  fileStamp // of the last load, for reload change detection
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{st.Size(), st.ModTime()}
}

// load reads the file and returns commands newest first (deduplicated, trivial
// and ihg's own invocations removed) with each command's newest timestamp.
func (s *source) load() (cmds []string, times []int64, err error) {
	stamp := stampOf(s.path)
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, nil, err
	}
	s.format = history.DetectFormat(s.path, data, s.shell)
	entries := history.ParseEntries(data, s.format)

	newest := map[string]int64{}
	var all []string // newest first
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !e.IsCommand || isSelfInvocation(e.Command) {
			continue
		}
		all = append(all, e.Command)
		if _, seen := newest[e.Command]; !seen {
			newest[e.Command] = e.Timestamp
		}
	}
	cmds = filters.BuildProcessed(all, s.ignore)
	times = make([]int64, len(cmds))
	for i, c := range cmds {
		times[i] = newest[c]
	}
	s.stamp = stamp
	return cmds, times, nil
}

// reload re-reads the file only if its size or modification time changed.
func (s *source) reload() (cmds []string, times []int64, changed bool, err error) {
	if stampOf(s.path) == s.stamp {
		return nil, nil, false, nil
	}
	cmds, times, err = s.load()
	return cmds, times, err == nil, err
}

func (s *source) delete(cmd string) (int, error) {
	n, err := history.Delete(s.path, cmd, s.format)
	if err == nil {
		// Our own rewrite should not look like an outside change.
		s.stamp = stampOf(s.path)
	}
	return n, err
}

// backup writes the cleaned history (deduplicated, trivial commands removed)
// oldest first to dest, or next to the history file when dest is empty.
func (s *source) backup(dest string) (string, error) {
	if dest == "" {
		dest = fmt.Sprintf("%s.ihg-clean.%s", s.path, time.Now().Format("20060102-150405"))
	} else if rest, ok := strings.CutPrefix(dest, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dest = home + "/" + rest
	}
	cmds, _, err := s.load()
	if err != nil {
		return "", err
	}
	slices.Reverse(cmds)
	return dest, history.WriteBackup(dest, cmds, s.format)
}

// copyWithTools is the UI's clipboard fallback after OSC 52. Having no tool
// at all (typical over SSH) is not an error worth reporting.
func copyWithTools(text string) (string, error) {
	tool, err := clipboard.CopyWithTools(context.Background(), text, clipboard.DefaultEnv())
	if errors.Is(err, clipboard.ErrNoTool) {
		return "", nil
	}
	return tool, err
}
