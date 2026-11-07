package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	dir, err := os.MkdirTemp("", "dot-merge")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	lf, bf, rf := filepath.Join(dir, "machine"), filepath.Join(dir, "base"), filepath.Join(dir, "repo")
	for p, d := range map[string][]byte{lf: lb, bf: bb, rf: rb} {
		if err := os.WriteFile(p, d, 0o600); err != nil {
			return nil, false, err
		}
	}
	out, err := exec.Command("git", "merge-file", "-p", "-L", "machine", "-L", baseLabel, "-L", "repo", lf, bf, rf).Output()
	var xe *exec.ExitError
	switch {
	case err == nil:
		return out, true, nil
	case errors.As(err, &xe) && xe.ExitCode() > 0 && xe.ExitCode() < 128:
		return out, false, nil
	}
	return nil, false, fmt.Errorf("git merge-file: %w", err)
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
