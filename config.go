package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Tool is one supported application, declared in tools.toml.
type Tool struct {
	Name         string   `toml:"-"`
	Description  string   `toml:"description"`
	Paths        []string `toml:"paths"`    // files or directories, relative to $HOME
	Ignore       []string `toml:"ignore"`   // globs on basename or relative path; "dir/**" excludes a subtree
	FontPatterns []string `toml:"fonts"`    // regexes whose first group is a font family name
	Brew         []string `toml:"brew"`     // Homebrew formulae the tool needs ("user/tap/name" auto-taps)
	Cask         []string `toml:"cask"`     // Homebrew casks the tool needs
	Requires     []string `toml:"requires"` // other tools `dot install` brings along with this one
}

// Font is one entry of fonts/fonts.toml.
type Font struct {
	Family  string `toml:"family"`
	Cask    string `toml:"cask"`    // Homebrew cask to install it
	Files   string `toml:"files"`   // directory under fonts/files with vendored font files
	Builtin bool   `toml:"builtin"` // shipped inside the app, nothing to install
}

func (f *Font) Source() string {
	switch {
	case f.Builtin:
		return "bundled with app"
	case f.Cask != "":
		return "cask " + f.Cask
	case f.Files != "":
		return "fonts/files/" + f.Files
	}
	return "no source"
}

// Repo is the dotfiles checkout plus this machine's sync state.
type Repo struct {
	Root     string
	Home     string
	StateDir string // per-machine: last-synced snapshots and backups
	Ignore   []string
	Tools    []*Tool
	Fonts    []*Font
	stamp    string
}

func findRoot() (string, error) {
	if d := os.Getenv("DOTFILES_DIR"); d != "" {
		return d, nil
	}
	var cands []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		d := filepath.Dir(exe)
		cands = append(cands, d, filepath.Dir(d))
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, wd)
	}
	home, _ := os.UserHomeDir()
	cands = append(cands, filepath.Join(home, "Projects", "dotfiles"), filepath.Join(home, ".dotfiles"))
	for _, c := range cands {
		if _, err := os.Stat(filepath.Join(c, "tools.toml")); err == nil {
			return c, nil
		}
	}
	return "", errors.New("cannot find the dotfiles repo (no tools.toml); set DOTFILES_DIR")
}

func LoadRepo() (*Repo, error) {
	root, err := findRoot()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	r := &Repo{
		Root:     root,
		Home:     home,
		StateDir: filepath.Join(state, "dotfiles"),
		stamp:    time.Now().Format("20060102-150405"),
	}

	var tf struct {
		Ignore []string         `toml:"ignore"`
		Tools  map[string]*Tool `toml:"tools"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "tools.toml"), &tf); err != nil {
		return nil, fmt.Errorf("tools.toml: %w", err)
	}
	r.Ignore = tf.Ignore
	for name, t := range tf.Tools {
		t.Name = name
		r.Tools = append(r.Tools, t)
	}
	sort.Slice(r.Tools, func(i, j int) bool { return r.Tools[i].Name < r.Tools[j].Name })
	for _, t := range r.Tools {
		for _, n := range t.Requires {
			if r.Tool(n) == nil {
				return nil, fmt.Errorf("tools.toml: %s requires unknown tool %q", t.Name, n)
			}
		}
	}

	var ff struct {
		Font []*Font `toml:"font"`
	}
	if _, err := toml.DecodeFile(r.fontsFile(), &ff); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("fonts.toml: %w", err)
	}
	r.Fonts = ff.Font
	return r, nil
}

func (r *Repo) fontsFile() string { return filepath.Join(r.Root, "fonts", "fonts.toml") }

func (r *Repo) Tilde(p string) string {
	if strings.HasPrefix(p, r.Home+"/") {
		return "~/" + strings.TrimPrefix(p, r.Home+"/")
	}
	return p
}

func matchAny(pats []string, rel string) bool {
	base := filepath.Base(rel)
	for _, p := range pats {
		if pre, ok := strings.CutSuffix(p, "/**"); ok {
			if rel == pre || strings.HasPrefix(rel, pre+"/") || strings.Contains("/"+rel, "/"+pre+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

func (r *Repo) ignored(t *Tool, rel string) bool {
	return matchAny(r.Ignore, rel) || matchAny(t.Ignore, rel)
}

// Tool returns the tool with that name, or nil.
func (r *Repo) Tool(name string) *Tool {
	for _, t := range r.Tools {
		if t.Name == name {
			return t
		}
	}
	return nil
}
