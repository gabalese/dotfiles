package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Selection is a set of tool names; nil means every tool.
type Selection map[string]bool

// Has reports whether any of the named tools is selected.
func (s Selection) Has(names ...string) bool {
	if s == nil {
		return true
	}
	for _, n := range names {
		if s[n] {
			return true
		}
	}
	return false
}

// Pkg is a Homebrew formula or cask a tool needs.
type Pkg struct {
	Name      string // as written in tools.toml; "user/tap/name" auto-taps
	Cask      bool
	Installed bool
	UsedBy    []string
}

func (p *Pkg) Kind() string {
	if p.Cask {
		return "cask"
	}
	return "brew"
}

func hasBrew() bool { _, err := exec.LookPath("brew"); return err == nil }

// brewList returns the short names of everything Homebrew has installed.
func brewList(cask bool) map[string]bool {
	flag := "--formula"
	if cask {
		flag = "--cask"
	}
	set := map[string]bool{}
	out, err := exec.Command("brew", "list", flag, "-1").Output()
	if err != nil {
		return set
	}
	for _, l := range strings.Fields(string(out)) {
		set[l] = true
	}
	return set
}

// caskApps maps each cask to the .app bundles it installs, so apps installed
// outside Homebrew (a downloaded DMG) still count as present.
func caskApps(casks []string) map[string][]string {
	apps := map[string][]string{}
	if len(casks) == 0 {
		return apps
	}
	out, err := exec.Command("brew", append([]string{"info", "--json=v2", "--cask"}, casks...)...).Output()
	if err != nil {
		return apps
	}
	var info struct {
		Casks []struct {
			Token     string           `json:"token"`
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"casks"`
	}
	if json.Unmarshal(out, &info) != nil {
		return apps
	}
	for _, c := range info.Casks {
		for _, a := range c.Artifacts {
			list, _ := a["app"].([]any)
			for _, v := range list {
				if s, ok := v.(string); ok {
					apps[c.Token] = append(apps[c.Token], s)
				}
			}
		}
	}
	return apps
}

// Packages lists every formula and cask declared in tools.toml, deduplicated,
// with whether this machine already has it — via Homebrew or otherwise
// (a binary of the same name on PATH, or the cask's app in /Applications).
func (r *Repo) Packages() []*Pkg {
	var pkgs []*Pkg
	byKey := map[string]*Pkg{}
	add := func(name string, cask bool, tool string) {
		key := map[bool]string{false: "f:", true: "c:"}[cask] + name
		if p := byKey[key]; p != nil {
			p.UsedBy = append(p.UsedBy, tool)
			return
		}
		p := &Pkg{Name: name, Cask: cask, UsedBy: []string{tool}}
		byKey[key] = p
		pkgs = append(pkgs, p)
	}
	for _, t := range r.Tools {
		for _, n := range t.Brew {
			add(n, false, t.Name)
		}
		for _, n := range t.Cask {
			add(n, true, t.Name)
		}
	}

	brew := hasBrew()
	var formulas, casks map[string]bool
	if brew {
		formulas, casks = brewList(false), brewList(true)
	}
	var unknownCasks []string
	for _, p := range pkgs {
		short := path.Base(p.Name)
		switch {
		case !p.Cask:
			_, onPath := exec.LookPath(short)
			p.Installed = formulas[short] || onPath == nil
		case casks[short]:
			p.Installed = true
		default:
			unknownCasks = append(unknownCasks, p.Name)
		}
	}
	if brew && len(unknownCasks) > 0 {
		apps := caskApps(unknownCasks)
		for _, p := range pkgs {
			if !p.Cask || p.Installed {
				continue
			}
			for _, app := range apps[path.Base(p.Name)] {
				for _, dir := range []string{"/Applications", filepath.Join(r.Home, "Applications")} {
					if _, err := os.Stat(filepath.Join(dir, app)); err == nil {
						p.Installed = true
					}
				}
			}
		}
	}
	return pkgs
}

// InstallCmd returns the brew command that installs p.
func (p *Pkg) InstallCmd() *exec.Cmd {
	if p.Cask {
		return exec.Command("brew", "install", "--cask", p.Name)
	}
	return exec.Command("brew", "install", p.Name)
}
