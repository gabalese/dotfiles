package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Status of one tracked file, comparing the repo copy, this machine's copy and
// the snapshot taken the last time the two were synced on this machine.
type Status int

const (
	Synced     Status = iota
	RepoOnly          // exists only in the repo            → apply to machine
	RepoAhead         // repo changed since last sync      → apply to machine
	LocalOnly         // exists only on this machine        → import into repo
	LocalAhead        // machine changed since last sync   → import into repo
	Diverged          // both changed, or never synced     → merge
)

func (s Status) String() string {
	return [...]string{"in sync", "repo only", "repo newer", "machine only", "machine newer", "both changed"}[s]
}

func (s Status) Symbol() string { return [...]string{"✓", "↓", "↓", "↑", "↑", "⇅"}[s] }

var ErrConflict = errors.New("merge has conflicts")
var ErrBinary = errors.New("binary file: choose apply or import instead of merge")

type Entry struct {
	Tool   *Tool
	Rel    string // path relative to $HOME
	Status Status
}

func (r *Repo) RepoPath(e *Entry) string {
	return filepath.Join(r.Root, "packages", e.Tool.Name, e.Rel)
}
func (r *Repo) LocalPath(e *Entry) string { return filepath.Join(r.Home, e.Rel) }
func (r *Repo) basePath(e *Entry) string {
	return filepath.Join(r.StateDir, "base", e.Tool.Name, e.Rel)
}

// Scan lists every file of a tool found in the repo or on the machine.
func (r *Repo) Scan(t *Tool) []*Entry {
	set := map[string]bool{}
	pkg := filepath.Join(r.Root, "packages", t.Name)
	for _, p := range t.Paths {
		p = filepath.Clean(p)
		collect(filepath.Join(r.Home, p), r.Home, set)
		collect(filepath.Join(pkg, p), pkg, set)
	}
	var out []*Entry
	for rel := range set {
		if r.ignored(t, rel) {
			continue
		}
		e := &Entry{Tool: t, Rel: rel}
		e.Status = r.status(e)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

func collect(path, root string, set map[string]bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	add := func(p string) {
		if rel, err := filepath.Rel(root, p); err == nil {
			set[rel] = true
		}
	}
	if fi.Mode().IsRegular() {
		add(path)
		return
	}
	if !fi.IsDir() {
		return
	}
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() { // follows symlinks, skips sockets
			add(p)
		}
		return nil
	})
}

func (r *Repo) status(e *Entry) Status {
	rb, rerr := os.ReadFile(r.RepoPath(e))
	lb, lerr := os.ReadFile(r.LocalPath(e))
	switch {
	case rerr != nil:
		return LocalOnly
	case lerr != nil:
		return RepoOnly
	case bytes.Equal(rb, lb):
		if bb, err := os.ReadFile(r.basePath(e)); err != nil || !bytes.Equal(bb, rb) {
			_ = r.writeBase(e, rb)
		}
		return Synced
	}
	bb, err := os.ReadFile(r.basePath(e))
	switch {
	case err != nil:
		return Diverged
	case bytes.Equal(lb, bb):
		return RepoAhead
	case bytes.Equal(rb, bb):
		return LocalAhead
	}
	return Diverged
}

// ── file operations ─────────────────────────────────────────────────────────

func writeFile(path string, data []byte, modeFrom string) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(modeFrom); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		_ = os.Remove(path) // replace links (e.g. from an older symlink setup) with a real file
	}
	tmp := path + ".dot-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *Repo) writeBase(e *Entry, data []byte) error {
	return writeFile(r.basePath(e), data, "")
}

func (r *Repo) backupLocal(e *Entry) error {
	data, err := os.ReadFile(r.LocalPath(e))
	if err != nil {
		return nil // nothing to back up
	}
	return writeFile(filepath.Join(r.BackupDir(), e.Tool.Name, e.Rel), data, r.LocalPath(e))
}

func (r *Repo) BackupDir() string { return filepath.Join(r.StateDir, "backup", r.stamp) }

// Backups lists the backup timestamps, oldest first.
func (r *Repo) Backups() []string {
	ds, _ := os.ReadDir(filepath.Join(r.StateDir, "backup"))
	var out []string
	for _, d := range ds {
		if d.IsDir() {
			out = append(out, d.Name())
		}
	}
	sort.Strings(out)
	return out
}

// BackupFiles lists what one backup holds. Tools are named as they were then.
func (r *Repo) BackupFiles(stamp string) []*Entry {
	root := filepath.Join(r.StateDir, "backup", stamp)
	tools, _ := os.ReadDir(root)
	var out []*Entry
	for _, d := range tools {
		if !d.IsDir() {
			continue
		}
		set := map[string]bool{}
		collect(filepath.Join(root, d.Name()), filepath.Join(root, d.Name()), set)
		t := &Tool{Name: d.Name()}
		for rel := range set {
			out = append(out, &Entry{Tool: t, Rel: rel})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

// Restore puts a backed-up file back on this machine, after backing up what
// is there now. The base snapshot is left alone, so the file then shows as
// changed on the machine. same reports that nothing needed restoring.
func (r *Repo) Restore(stamp string, e *Entry) (same bool, err error) {
	src := filepath.Join(r.StateDir, "backup", stamp, e.Tool.Name, e.Rel)
	data, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	if cur, err := os.ReadFile(r.LocalPath(e)); err == nil && bytes.Equal(cur, data) {
		return true, nil
	}
	if err := r.backupLocal(e); err != nil {
		return false, err
	}
	return false, writeFile(r.LocalPath(e), data, src)
}

// Apply copies the repo version onto this machine.
func (r *Repo) Apply(e *Entry) error {
	data, err := os.ReadFile(r.RepoPath(e))
	if err != nil {
		return fmt.Errorf("%s is not in the repo", e.Rel)
	}
	if err := r.backupLocal(e); err != nil {
		return err
	}
	if err := writeFile(r.LocalPath(e), data, r.RepoPath(e)); err != nil {
		return err
	}
	return r.writeBase(e, data)
}

// Import copies this machine's version into the repo.
func (r *Repo) Import(e *Entry) error {
	data, err := os.ReadFile(r.LocalPath(e))
	if err != nil {
		return fmt.Errorf("%s is not on this machine", r.Tilde(r.LocalPath(e)))
	}
	if err := writeFile(r.RepoPath(e), data, r.LocalPath(e)); err != nil {
		return err
	}
	return r.writeBase(e, data)
}

// Resolve writes the same content to both sides.
func (r *Repo) Resolve(e *Entry, data []byte) error {
	if err := r.backupLocal(e); err != nil {
		return err
	}
	if err := writeFile(r.LocalPath(e), data, r.LocalPath(e)); err != nil {
		return err
	}
	if err := writeFile(r.RepoPath(e), data, r.LocalPath(e)); err != nil {
		return err
	}
	return r.writeBase(e, data)
}

func isBinary(b []byte) bool {
	if len(b) > 8192 {
		b = b[:8192]
	}
	return bytes.IndexByte(b, 0) >= 0
}

// Merge does a three-way merge of machine and repo copies. The base is the
// last-synced snapshot; without one it is the lines both files share, so that
// additions from either side are kept and only real disagreements conflict.
func (r *Repo) Merge(e *Entry) (merged []byte, clean bool, err error) {
	rb, err := os.ReadFile(r.RepoPath(e))
	if err != nil {
		return nil, false, err
	}
	lb, err := os.ReadFile(r.LocalPath(e))
	if err != nil {
		return nil, false, err
	}
	if isBinary(rb) || isBinary(lb) {
		return nil, false, ErrBinary
	}
	baseLabel := "last sync"
	bb, err := os.ReadFile(r.basePath(e))
	if err != nil {
		baseLabel = "common"
		if bb, err = commonLines(r.RepoPath(e), r.LocalPath(e)); err != nil {
			return nil, false, err
		}
	}
	out, n, err := mergeFile(lb, bb, rb, baseLabel)
	if err != nil {
		return nil, false, err
	}
	return out, n == 0, nil
}

// mergeFile runs `git merge-file machine base repo` and returns the result
// with the number of conflicting hunks. Extra flags (--ours, --theirs) make
// git settle those hunks itself, in which case the count is 0.
func mergeFile(lb, bb, rb []byte, baseLabel string, flags ...string) ([]byte, int, error) {
	dir, err := os.MkdirTemp("", "dot-merge")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	lf, bf, rf := filepath.Join(dir, "machine"), filepath.Join(dir, "base"), filepath.Join(dir, "repo")
	for p, d := range map[string][]byte{lf: lb, bf: bb, rf: rb} {
		if err := os.WriteFile(p, d, 0o600); err != nil {
			return nil, 0, err
		}
	}
	args := append([]string{"merge-file", "-p"}, flags...)
	args = append(args, "-L", "machine", "-L", baseLabel, "-L", "repo", lf, bf, rf)
	out, err := exec.Command("git", args...).Output()
	var xe *exec.ExitError
	switch {
	case err == nil:
		return out, 0, nil
	case errors.As(err, &xe) && xe.ExitCode() > 0 && xe.ExitCode() < 128:
		return out, xe.ExitCode(), nil
	}
	return nil, 0, fmt.Errorf("git merge-file: %w", err)
}

// Side is the copy that wins where an automatic merge cannot keep both.
type Side int

const (
	Machine Side = iota
	RepoSide
)

func (s Side) String() string { return [...]string{"machine", "repo"}[s] }

// Forced says what ForceMerge had to decide by itself for one file.
type Forced struct {
	Magic int    // conflicting hunks merged word by word, keeping both sides
	Taken int    // conflicting hunks taken from the preferred side
	Whole string // why the whole file was taken from the preferred side, if it was
}

// ForceMerge is Auto without the need for a human. Changes from both sides
// are merged as usual. Hunks that conflict line by line are merged word by
// word ("magic merge": two edits to different parts of the same lines both
// survive); only where the two sides changed the same words is the hunk taken
// from the preferred side. Files that cannot be merged at all (binary), or
// whose merge would no longer parse as JSON/TOML, are taken whole from it.
func (r *Repo) ForceMerge(e *Entry, prefer Side) (f Forced, err error) {
	if e.Status != Diverged {
		return f, r.Auto(e)
	}
	whole := func(why string) (Forced, error) {
		if prefer == RepoSide {
			return Forced{Whole: why}, r.Apply(e)
		}
		return Forced{Whole: why}, r.Import(e)
	}
	rb, err := os.ReadFile(r.RepoPath(e))
	if err != nil {
		return f, err
	}
	lb, err := os.ReadFile(r.LocalPath(e))
	if err != nil {
		return f, err
	}
	if isBinary(rb) || isBinary(lb) {
		return whole("binary")
	}
	bb, err := os.ReadFile(r.basePath(e))
	if err != nil {
		if bb, err = commonLines(r.RepoPath(e), r.LocalPath(e)); err != nil {
			return f, err
		}
	}
	merged, magic, taken, err := magicMerge(lb, bb, rb, prefer)
	if err != nil {
		return f, err
	}
	if parses(e.Rel, lb) && parses(e.Rel, rb) && !parses(e.Rel, merged) {
		return whole("merge would break the " + strings.TrimPrefix(filepath.Ext(e.Rel), ".") + " syntax")
	}
	return Forced{Magic: magic, Taken: taken}, r.Resolve(e, merged)
}

// Conflict markers long enough that no config line is mistaken for one.
const markerSize = 40

var (
	markOurs   = strings.Repeat("<", markerSize) + " "
	markBase   = strings.Repeat("|", markerSize) + " "
	markSplit  = strings.Repeat("=", markerSize)
	markTheirs = strings.Repeat(">", markerSize) + " "
)

// magicMerge merges machine and repo over base and settles every conflict:
// each conflicting hunk is merged again word by word, and if that still
// conflicts it is taken from the preferred side.
func magicMerge(lb, bb, rb []byte, prefer Side) (out []byte, magic, taken int, err error) {
	sideFlag := "--ours" // argument order is machine base repo
	if prefer == RepoSide {
		sideFlag = "--theirs"
	}
	plain := func() ([]byte, int, int, error) { // no word-level pass
		out, n, err := mergeFile(lb, bb, rb, "base")
		if err != nil || n == 0 {
			return out, 0, 0, err
		}
		out, _, err = mergeFile(lb, bb, rb, "base", sideFlag)
		return out, 0, n, err
	}
	for _, in := range [][]byte{lb, bb, rb} { // a line that would read as a marker
		for _, line := range bytes.Split(in, []byte("\n")) {
			l := string(line)
			if strings.HasPrefix(l, markOurs) || strings.HasPrefix(l, markBase) || strings.HasPrefix(l, markTheirs) ||
				strings.TrimRight(l, "\r") == markSplit {
				return plain()
			}
		}
	}
	marked, n, err := mergeFile(lb, bb, rb, "base", "--zdiff3", fmt.Sprintf("--marker-size=%d", markerSize))
	if err != nil {
		return nil, 0, 0, err
	}
	if n == 0 {
		return marked, 0, 0, nil
	}
	var buf, ours, base, theirs bytes.Buffer
	cur := &buf
	for _, line := range bytes.SplitAfter(marked, []byte("\n")) {
		l := string(line)
		switch {
		case cur == &buf && strings.HasPrefix(l, markOurs):
			cur = &ours
		case cur == &ours && strings.HasPrefix(l, markBase):
			cur = &base
		case (cur == &ours || cur == &base) && strings.TrimRight(l, "\r\n") == markSplit:
			cur = &theirs
		case cur == &theirs && strings.HasPrefix(l, markTheirs):
			if words, ok := mergeWords(ours.Bytes(), base.Bytes(), theirs.Bytes()); ok {
				magic++
				buf.Write(words)
			} else if taken++; prefer == RepoSide {
				buf.Write(theirs.Bytes())
			} else {
				buf.Write(ours.Bytes())
			}
			ours.Reset()
			base.Reset()
			theirs.Reset()
			cur = &buf
		default:
			cur.Write(line)
		}
	}
	if cur != &buf || magic+taken != n {
		return plain() // markers did not add up; do not guess
	}
	return buf.Bytes(), magic, taken, nil
}

// mergeWords three-way merges one hunk token by token. It succeeds when the
// two sides changed different words.
func mergeWords(ours, base, theirs []byte) ([]byte, bool) {
	if !utf8.Valid(ours) || !utf8.Valid(base) || !utf8.Valid(theirs) {
		return nil, false // tokenising would mangle the bytes
	}
	out, n, err := mergeFile(tokenLines(ours), tokenLines(base), tokenLines(theirs), "base")
	if err != nil || n > 0 {
		return nil, false
	}
	var buf bytes.Buffer
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if l == "" {
			continue
		}
		tok, err := strconv.Unquote(l)
		if err != nil {
			return nil, false
		}
		buf.WriteString(tok)
	}
	return buf.Bytes(), true
}

// tokenLines splits text into words, runs of blanks and single other
// characters, and writes one quoted token per line so that a line merge of
// the result is a word merge of the text.
func tokenLines(text []byte) []byte {
	class := func(r rune) int {
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			return 1
		case r == ' ' || r == '\t':
			return 2
		}
		return 0 // punctuation and newlines stand alone
	}
	var buf bytes.Buffer
	rs := []rune(string(text))
	for i := 0; i < len(rs); {
		j := i + 1
		if c := class(rs[i]); c != 0 {
			for j < len(rs) && class(rs[j]) == c {
				j++
			}
		}
		buf.WriteString(strconv.Quote(string(rs[i:j])))
		buf.WriteByte('\n')
		i = j
	}
	return buf.Bytes()
}

// parses reports whether data is valid for the format its extension names.
// Unknown formats never parse, so the check is skipped for them.
func parses(rel string, data []byte) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".json":
		return json.Valid(data)
	case ".toml":
		var v map[string]any
		return toml.Unmarshal(data, &v) == nil
	}
	return false
}

func commonLines(a, b string) ([]byte, error) {
	out, _ := exec.Command("git", "--no-pager", "diff", "--no-index", "--no-color", "-U999999", "--", a, b).Output()
	if len(out) == 0 {
		return os.ReadFile(a)
	}
	var buf bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	started := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "@@") {
			started = true
			continue
		}
		if started && strings.HasPrefix(line, " ") {
			buf.WriteString(line[1:])
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), sc.Err()
}

func HasConflictMarkers(b []byte) bool {
	for _, l := range strings.Split(string(b), "\n") {
		for _, m := range []string{"<<<<<<< ", "=======", ">>>>>>> "} {
			if l == strings.TrimSpace(m) || strings.HasPrefix(l, m) {
				return true
			}
		}
	}
	return false
}

// Auto does the obvious thing for an entry. It returns ErrConflict when a
// merge needs a human.
func (r *Repo) Auto(e *Entry) error {
	switch e.Status {
	case RepoOnly, RepoAhead:
		return r.Apply(e)
	case LocalOnly, LocalAhead:
		return r.Import(e)
	case Diverged:
		merged, clean, err := r.Merge(e)
		if err != nil {
			return err
		}
		if !clean {
			return ErrConflict
		}
		return r.Resolve(e, merged)
	}
	return nil
}

// Diff renders repo → machine as a colored unified diff.
func (r *Repo) Diff(e *Entry) string {
	a, b := r.RepoPath(e), r.LocalPath(e)
	if _, err := os.Stat(a); err != nil {
		a = "/dev/null"
	}
	if _, err := os.Stat(b); err != nil {
		b = "/dev/null"
	}
	out, _ := exec.Command("git", "--no-pager", "diff", "--no-index", "--color=always",
		"--src-prefix=repo:", "--dst-prefix=machine:", "--", a, b).Output()
	if len(out) == 0 {
		return "(no differences)"
	}
	return string(out)
}
