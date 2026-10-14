// Package history reads and rewrites bash and zsh history files.
//
// History files are byte streams, not text; commands are held in Go strings,
// which may contain arbitrary bytes, so undecodable bytes survive a parse ->
// rewrite round trip unchanged. The package never reads environment variables:
// callers pass the shell path where it matters.
package history

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Format identifies the on-disk history format.
type Format int

const (
	// FormatBash is bash's format: optional "#<unix-ts>" lines, one command per line.
	FormatBash Format = iota
	// FormatZsh is zsh's format, plain or EXTENDED_HISTORY (": ts:elapsed;cmd"),
	// metafied and with backslash-newline continuations.
	FormatZsh
)

// Entry is one logical region of a history file.
type Entry struct {
	// Command is the trimmed command text; empty when IsCommand is false.
	Command string
	// Timestamp is the unix time recorded for the entry (zsh extended header
	// or bash "#ts" line), or 0 if none.
	Timestamp int64
	// Raw is the exact file bytes of the entry. Concatenating the Raw of all
	// entries reproduces the input exactly.
	Raw []byte
	// IsCommand is false for regions that are not commands (blank lines,
	// comments or garbage in extended zsh files) which are preserved verbatim.
	IsCommand bool
}

var bashTimestampLine = regexp.MustCompile(`(?m)^#\d+\r?$`)

// DetectFormat decides whether the history file at path (with contents data)
// is zsh or bash. Order: zsh extended headers; bash "#<digits>" timestamp
// lines; the symlink-resolved file name ("zsh"/"zhistory" -> zsh, "bash" ->
// bash); finally whether the base name of shell is "zsh".
func DetectFormat(path string, data []byte, shell string) Format {
	if IsZshExtended(data) {
		return FormatZsh
	}
	if bashTimestampLine.Match(data) {
		return FormatBash
	}
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}
	name := strings.ToLower(filepath.Base(resolved))
	if strings.Contains(name, "zsh") || strings.Contains(name, "zhistory") {
		return FormatZsh
	}
	if strings.Contains(name, "bash") {
		return FormatBash
	}
	if strings.ToLower(filepath.Base(shell)) == "zsh" {
		return FormatZsh
	}
	return FormatBash
}

// ParseEntries splits data into entries according to f.
func ParseEntries(data []byte, f Format) []Entry {
	if f == FormatZsh {
		return zshEntries(data)
	}
	return bashEntries(data)
}

// Commands returns the commands of entries, in file order (oldest first).
func Commands(entries []Entry) []string {
	var out []string
	for _, e := range entries {
		if e.IsCommand {
			out = append(out, e.Command)
		}
	}
	return out
}

// Load reads the history file at path and returns its commands oldest first
// together with the detected format. shell is the user's shell path, used only
// as the last-resort format hint (see DetectFormat).
func Load(path, shell string) (cmds []string, f Format, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, FormatBash, err
	}
	f = DetectFormat(path, data, shell)
	return Commands(ParseEntries(data, f)), f, nil
}
