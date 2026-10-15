package history

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// "→" is E2 86 92; zsh stores 0x86/0x92 as 0x83 0xa6 / 0x83 0xb2.
const arrowOnDisk = "\xe2\x83\xa6\x83\xb2"

var tricky = []string{
	"printf 'a\\nb'",
	"echo café → ✓",
	"for i in 1 2; do\n  echo $i\ndone",
	"echo trailing\\",
	"echo bs space\\ ",
	"line\\\nnext",
	"echo \x83\xa2 raw-bytes", // undecodable 0x83 0xa2
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func cmds(data string, f Format) []string { return Commands(ParseEntries([]byte(data), f)) }

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		data string
		f    Format
		want []string
	}{
		{"zsh basic", ": 1320301015:0;cd /tmp\n: 1320301016:0;ls -la\n", FormatZsh, []string{"cd /tmp", "ls -la"}},
		{"zsh literal backslash-n", ": 1:0;printf 'a\\nb'\n", FormatZsh, []string{"printf 'a\\nb'"}},
		{"zsh metafied utf8", ": 1:0;echo " + arrowOnDisk + "\n", FormatZsh, []string{"echo →"}},
		{"zsh multiline", ": 1:0;for i in 1 2; do\\\n  echo $i\\\ndone\n: 2:0;next\n", FormatZsh,
			[]string{"for i in 1 2; do\n  echo $i\ndone", "next"}},
		{"zsh trailing backslash guard", ": 1:0;echo x\\ \n: 2:0;y\n", FormatZsh, []string{"echo x\\", "y"}},
		{"bash plain", "echo one\necho two\n", FormatBash, []string{"echo one", "echo two"}},
		{"bash timestamp", "#1234567890\necho dated\n#1234567891\necho next\n", FormatBash, []string{"echo dated", "echo next"}},
		{"bash continuation", "foo \\\n bar\nnext\n", FormatBash, []string{"foo \\\n bar", "next"}},
		{"bash blocks match", "#1\necho a\n#2\necho b\necho c\n", FormatBash, []string{"echo a", "echo b", "echo c"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmds(tc.data, tc.f); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTimestamps(t *testing.T) {
	z := ParseEntries([]byte(": 1320301015:0;a\n: 7:3;b\n"), FormatZsh)
	if z[0].Timestamp != 1320301015 || z[1].Timestamp != 7 {
		t.Errorf("zsh timestamps: %+v", z)
	}
	b := ParseEntries([]byte("#1234567890\necho dated\nplain\n"), FormatBash)
	if b[0].Timestamp != 1234567890 || b[1].Timestamp != 0 {
		t.Errorf("bash timestamps: %+v", b)
	}
}

func TestRawConcatenatesToInput(t *testing.T) {
	inputs := []string{
		"", "\n", "a", "a\n", ": 1:0;a\n# c\n\n: 2:0;m\\\nl", "x\\", "x\\\n", "#1\na\n \n#2",
		"echo \xff\xfe\n\n#1700000000\necho dated\n", "#5\n", "#5", "a\n b\n\n",
	}
	for _, in := range inputs {
		for _, f := range []Format{FormatBash, FormatZsh} {
			var buf []byte
			for _, e := range ParseEntries([]byte(in), f) {
				buf = append(buf, e.Raw...)
			}
			if string(buf) != in {
				t.Errorf("format %d input %q: reassembled %q", f, in, buf)
			}
		}
	}
}

func TestIsZshExtended(t *testing.T) {
	if !IsZshExtended([]byte(": 1:0;a\n: 2:0;b\n")) {
		t.Error("extended not detected")
	}
	if IsZshExtended([]byte("ls\npwd\n")) || IsZshExtended(nil) {
		t.Error("plain detected as extended")
	}
}

func TestMetafy(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	if !bytes.Equal(Unmetafy(Metafy(all)), all) {
		t.Error("round trip of all 256 bytes failed")
	}
	if string(Metafy([]byte("→"))) != arrowOnDisk {
		t.Errorf("arrow metafied to %x", Metafy([]byte("→")))
	}
}

func TestEncodeThenParseRoundTrip(t *testing.T) {
	for _, c := range tricky {
		for _, ext := range []bool{true, false} {
			got := cmds(string(encodeZshEntry(c, 1, ext)), FormatZsh)
			if want := []string{strings.TrimSpace(c)}; !reflect.DeepEqual(got, want) {
				t.Errorf("%q extended=%v: got %q", c, ext, got)
			}
		}
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name, body, shell string
		want              Format
	}{
		{"hist", ": 1:0;a\n", "/bin/bash", FormatZsh},
		{"hist", "#1700000000\na\n", "/bin/zsh", FormatBash},
		{".zsh_history", "a\n", "/bin/bash", FormatZsh},
		{".zhistory", "a\n", "/bin/bash", FormatZsh},
		{".bash_history", "a\n", "/bin/zsh", FormatBash},
		{"hist", "a\n", "/bin/zsh", FormatZsh},
		{"hist", "a\n", "/bin/bash", FormatBash},
		{"hist", "a\n", "", FormatBash},
	}
	for _, tc := range tests {
		p := filepath.Join(t.TempDir(), tc.name)
		writeFile(t, p, tc.body)
		if got := DetectFormat(p, []byte(tc.body), tc.shell); got != tc.want {
			t.Errorf("%s %q shell=%s: got %d want %d", tc.name, tc.body, tc.shell, got, tc.want)
		}
	}
}

func TestDetectFormatResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, ".zsh_history")
	writeFile(t, real, "a\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if DetectFormat(link, []byte("a\n"), "/bin/bash") != FormatZsh {
		t.Error("symlink target name not used")
	}
}

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".zsh_history")
	writeFile(t, p, "echo one\nfor x in a; do\\\n  echo "+arrowOnDisk+"\\\ndone\n")
	got, f, err := Load(p, "")
	if err != nil || f != FormatZsh {
		t.Fatalf("err=%v f=%d", err, f)
	}
	if want := []string{"echo one", "for x in a; do\n  echo →\ndone"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	if _, _, err := Load(filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestAppendAndDeleteZsh(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, ": 1:0;keep\n: 2:0;drop\n")
	if err := Append(p, "newcmd", FormatZsh); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, p), "newcmd") {
		t.Fatal("append missing")
	}
	n, err := Delete(p, "drop", FormatZsh)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	text := readFile(t, p)
	if strings.Contains(text, "drop") || !strings.Contains(text, "keep") || !strings.Contains(text, ": ") {
		t.Errorf("unexpected %q", text)
	}
}

func TestAppendAndDeleteBash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, "one\ntwo")
	if err := Append(p, "three\n", FormatBash); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "one\ntwo\nthree\n" {
		t.Fatalf("got %q", got)
	}
	n, err := Delete(p, "two", FormatBash)
	if err != nil || n != 1 || readFile(t, p) != "one\nthree\n" {
		t.Fatalf("n=%d err=%v body=%q", n, err, readFile(t, p))
	}
	if err := Append(p, "  \n", FormatBash); err != nil || readFile(t, p) != "one\nthree\n" {
		t.Error("blank append should be a no-op")
	}
}

func TestDeleteZshPreservesNonEntries(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, "# comment\n: 1:0;x\n\n")
	if _, err := Delete(p, "x", FormatZsh); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "# comment\n\n" {
		t.Errorf("got %q", got)
	}
}

func TestWriteBackup(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		f    Format
		cmds []string
	}{
		{FormatBash, []string{"cd /tmp", "ls", "pwd"}},
		{FormatZsh, []string{"cd /tmp", "ls", "multi\nline", "echo →"}},
	}
	for _, tc := range tests {
		dest := filepath.Join(dir, "sub", "out")
		if err := WriteBackup(dest, tc.cmds, tc.f); err != nil {
			t.Fatal(err)
		}
		body := readFile(t, dest)
		if (tc.f == FormatZsh) != IsZshExtended([]byte(body)) {
			t.Errorf("format %d: wrong extended-ness", tc.f)
		}
		if got := cmds(body, tc.f); !reflect.DeepEqual(got, tc.cmds) {
			t.Errorf("format %d: got %q", tc.f, got)
		}
	}
}

func TestDeleteKeepsOtherEntriesByteExactZsh(t *testing.T) {
	keep := ": 1:0;echo caf\xc3\xa9\n: 2:0;echo " + arrowOnDisk + "\n: 3:0;multi\\\nline\ngarbage line\n"
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, keep+": 4:0;git status\n")
	if n, err := Delete(p, "git status", FormatZsh); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := readFile(t, p); got != keep {
		t.Errorf("got %q", got)
	}
}

func TestDeleteMultilineEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, ": 1:0;a\n: 2:0;multi\\\nline\n: 3:0;b\n")
	if n, err := Delete(p, "multi\nline", FormatZsh); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := readFile(t, p); got != ": 1:0;a\n: 3:0;b\n" {
		t.Errorf("got %q", got)
	}
}

func TestDeleteBashByteExact(t *testing.T) {
	keep := "echo \xff\xfe legacy\n\n#1700000000\necho dated\n"
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, keep+"rm me\n")
	if n, err := Delete(p, "rm me", FormatBash); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := readFile(t, p); got != keep {
		t.Errorf("got %q", got)
	}
}

func TestDeletePreservesSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real_hist")
	writeFile(t, real, ": 1:0;a\n: 2:0;b\n")
	if err := os.Chmod(real, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link_hist")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if n, err := Delete(link, "a", FormatZsh); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Error("link is no longer a symlink")
	}
	if readFile(t, real) != ": 2:0;b\n" {
		t.Error("wrong contents")
	}
	if st, _ := os.Stat(real); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".ihg") || strings.HasSuffix(e.Name(), ".LOCK") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestDeleteWaitsForFreshLock(t *testing.T) {
	old := lockTimeout
	lockTimeout = 100 * time.Millisecond
	defer func() { lockTimeout = old }()
	dir := t.TempDir()
	p := filepath.Join(dir, "hist")
	writeFile(t, p, ": 1:0;a\n")
	writeFile(t, p+".LOCK", "1 host\n")
	_, err := Delete(p, "a", FormatZsh)
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err=%v", err)
	}
	if readFile(t, p) != ": 1:0;a\n" {
		t.Error("file modified")
	}
}

func TestStaleLockTakenOver(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hist")
	writeFile(t, p, ": 1:0;a\n: 2:0;b\n")
	lock := p + ".LOCK"
	writeFile(t, lock, "1 host\n")
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if n, err := Delete(p, "a", FormatZsh); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Error("lock not removed")
	}
}

func TestAppendZshDoesNotDoubleBackslashes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist")
	writeFile(t, p, ": 1:0;a\n")
	if err := Append(p, "printf 'x\\ny'", FormatZsh); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(readFile(t, p), ":0;printf 'x\\ny'\n") {
		t.Errorf("got %q", readFile(t, p))
	}
	got, _, _ := Load(p, "/bin/zsh")
	if want := []string{"a", "printf 'x\\ny'"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestPlainZshAppendStaysPlain(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".zsh_history")
	writeFile(t, p, "echo one\n")
	if err := Append(p, "echo two", FormatZsh); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "echo one\necho two\n" {
		t.Errorf("got %q", got)
	}
}

func TestEmptyZshAppendIsExtended(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h")
	writeFile(t, p, "")
	if err := Append(p, "echo hi", FormatZsh); err != nil {
		t.Fatal(err)
	}
	if !IsZshExtended([]byte(readFile(t, p))) {
		t.Error("expected extended form")
	}
}

// zshOracleEnv is the minimal environment for the real-zsh checks.
func zshOracleEnv(home string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
}

// zshReads returns the commands zsh itself reads from histfile.
func zshReads(t *testing.T, zsh, home, histfile string) []string {
	t.Helper()
	cmd := exec.Command(zsh, "-f", "-i", "-c",
		`zmodload zsh/parameter; HISTSIZE=1000000; fc -R "$1"; `+
			`for ((i = 1; i <= HISTCMD; i++)); do `+
			`(( ${+history[$i]} )) && print -rn -- "${history[$i]}"$'\0'; done`,
		"zsh", histfile)
	cmd.Env = zshOracleEnv(home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("zsh failed: %v", err)
	}
	var got []string
	for _, b := range bytes.Split(out, []byte{0}) {
		got = append(got, string(b))
	}
	return got[:len(got)-1]
}

// zshWritesBackslashGuard reports whether zsh appends a guard space when
// saving a command that ends in a backslash (newer zsh does; 5.5.1 does not).
func zshWritesBackslashGuard(t *testing.T, zsh string) bool {
	t.Helper()
	dir := t.TempDir()
	hist := filepath.Join(dir, "h")
	cmd := exec.Command(zsh, "-f", "-i", "-c",
		`HISTSIZE=10 SAVEHIST=10 HISTFILE="$1"; print -rs -- 'x\'; fc -W`, "zsh", hist)
	cmd.Env = zshOracleEnv(dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zsh probe: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(hist)
	return bytes.HasSuffix(data, []byte("\\ \n"))
}

func TestRealZshAgreesBothDirections(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	cases := tricky
	if !zshWritesBackslashGuard(t, zsh) {
		// Older zsh (seen on 5.5.1) writes "cmd\\" with no guard space and so
		// merges it with the next entry when reading its own file back; ihg
		// follows the fixed format, so those cases cannot round-trip there.
		t.Log("this zsh lacks the trailing-backslash guard; skipping those cases")
		cases = slices.DeleteFunc(slices.Clone(tricky), func(c string) bool {
			return strings.HasSuffix(strings.TrimRight(c, " "), `\`)
		})
	}
	for _, extended := range []bool{true, false} {
		name := map[bool]string{true: "extended", false: "plain"}[extended]
		t.Run(name, func(t *testing.T) {
			tricky := cases
			dir := t.TempDir()
			cmdsFile := filepath.Join(dir, "cmds")
			writeFile(t, cmdsFile, strings.Join(tricky, "\x00")+"\x00")

			// 1. zsh writes, we parse.
			zfile := filepath.Join(dir, ".zsh_history")
			opt := ""
			if extended {
				opt = "setopt extended_history; "
			}
			cmd := exec.Command(zsh, "-f", "-i", "-c",
				opt+`HISTSIZE=100 SAVEHIST=100 HISTFILE="$1"; `+
					`while IFS= read -r -d '' c; do print -rs -- "$c"; done < "$2"; fc -W`,
				"zsh", zfile, cmdsFile)
			cmd.Env = zshOracleEnv(dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("zsh: %v\n%s", err, out)
			}
			data, _ := os.ReadFile(zfile)
			if IsZshExtended(data) != extended {
				t.Fatalf("extended detection = %v", !extended)
			}
			got, f, err := Load(zfile, "")
			if err != nil || f != FormatZsh {
				t.Fatalf("err=%v f=%d", err, f)
			}
			var want []string
			for _, c := range tricky {
				want = append(want, strings.TrimSpace(c))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("we parsed %q, want %q", got, want)
			}

			// 2. we write, zsh reads.
			var buf []byte
			for _, c := range tricky {
				buf = append(buf, encodeZshEntry(c, 1, extended)...)
			}
			ifile := filepath.Join(dir, "ihg_written")
			if err := os.WriteFile(ifile, buf, 0o644); err != nil {
				t.Fatal(err)
			}
			if got := zshReads(t, zsh, dir, ifile); !reflect.DeepEqual(got, tricky) {
				t.Errorf("zsh read %q, want %q", got, tricky)
			}
		})
	}
}
