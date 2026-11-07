package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FontRow is a font that is either listed in fonts.toml or referenced by a config.
type FontRow struct {
	Family    string
	Font      *Font // nil when referenced by a config but missing from fonts.toml
	Installed bool
	UsedBy    []string
}

func installedFamilies() map[string]bool {
	out, _ := exec.Command("osascript", "-l", "JavaScript", "-e",
		`ObjC.import("AppKit"); ObjC.deepUnwrap($.NSFontManager.sharedFontManager.availableFontFamilies).join("\n")`).Output()
	have := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			have[l] = true
		}
	}
	return have
}

// usedFonts scans the repo copy of every tool's config for font families.
func (r *Repo) usedFonts() map[string][]string {
	used := map[string][]string{}
	for _, t := range r.Tools {
		var res []*regexp.Regexp
		for _, p := range t.FontPatterns {
			if re, err := regexp.Compile(p); err == nil {
				res = append(res, re)
			}
		}
		if len(res) == 0 {
			continue
		}
		pkg := filepath.Join(r.Root, "packages", t.Name)
		_ = filepath.WalkDir(pkg, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(pkg, p)
			if r.ignored(t, rel) {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil || isBinary(data) {
				return nil
			}
			for _, line := range strings.Split(string(data), "\n") {
				for _, re := range res {
					m := re.FindStringSubmatch(line)
					if len(m) < 2 {
						continue
					}
					fam := strings.TrimSpace(strings.ReplaceAll(m[1], `\ `, " "))
					fam = strings.Trim(fam, `"'`)
					if fam == "" {
						continue
					}
					found := false
					for _, u := range used[fam] {
						found = found || u == t.Name
					}
					if !found {
						used[fam] = append(used[fam], t.Name)
					}
				}
			}
			return nil
		})
	}
	return used
}

func (r *Repo) FontRows() []*FontRow {
	have := installedFamilies()
	used := r.usedFonts()
	var rows []*FontRow
	seen := map[string]bool{}
	for _, f := range r.Fonts {
		rows = append(rows, &FontRow{Family: f.Family, Font: f, Installed: have[f.Family], UsedBy: used[f.Family]})
		seen[f.Family] = true
	}
	var extra []string
	for fam := range used {
		if !seen[fam] {
			extra = append(extra, fam)
		}
	}
	sort.Strings(extra)
	for _, fam := range extra {
		rows = append(rows, &FontRow{Family: fam, Installed: have[fam], UsedBy: used[fam]})
	}
	return rows
}

func (fr *FontRow) NeedsInstall() bool {
	return fr.Font != nil && !fr.Installed && !fr.Font.Builtin
}

// InstallFont installs a listed font and returns any command output.
func (r *Repo) InstallFont(f *Font) (string, error) {
	switch {
	case f.Builtin:
		return "", nil
	case f.Cask != "":
		out, err := exec.Command("brew", "install", "--cask", f.Cask).CombinedOutput()
		return string(out), err
	case f.Files != "":
		dst := filepath.Join(r.Home, "Library", "Fonts")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return "", err
		}
		files, _ := filepath.Glob(filepath.Join(r.Root, "fonts", "files", f.Files, "*"))
		n := 0
		for _, src := range files {
			data, err := os.ReadFile(src)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(dst, filepath.Base(src)), data, 0o644); err != nil {
				return "", err
			}
			n++
		}
		if n == 0 {
			return "", fmt.Errorf("no files in fonts/files/%s", f.Files)
		}
		return fmt.Sprintf("copied %d files", n), nil
	}
	return "", fmt.Errorf("%s has no source in fonts.toml", f.Family)
}

func caskCandidates(fam string) []string {
	norm := func(s string) string {
		s = strings.ToLower(s)
		s = regexp.MustCompile(`nerd font( mono| propo)?$`).ReplaceAllString(s, "nerd font")
		s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
		return "font-" + strings.Trim(s, "-")
	}
	var out []string
	for _, v := range []string{
		fam,
		regexp.MustCompile(`([a-z])(Mono|Sans|Serif|Code)`).ReplaceAllString(fam, "$1 $2"),
		regexp.MustCompile(`([a-z])([A-Z])`).ReplaceAllString(fam, "$1 $2"),
	} {
		c := norm(v)
		dup := false
		for _, o := range out {
			dup = dup || o == c
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// AddFont records a font in fonts.toml: via a Homebrew cask when one exists,
// otherwise by vendoring the installed font files into fonts/files.
func (r *Repo) AddFont(fam string) (*Font, error) {
	f := &Font{Family: fam}
	if _, err := exec.LookPath("brew"); err == nil {
		for _, c := range caskCandidates(fam) {
			if exec.Command("brew", "info", "--cask", c).Run() == nil {
				f.Cask = c
				break
			}
		}
	}
	if f.Cask == "" {
		out, _ := exec.Command("mdfind", "-onlyin", filepath.Join(r.Home, "Library", "Fonts"), "-onlyin", "/Library/Fonts",
			"com_apple_ats_name_family == '"+strings.ReplaceAll(fam, "'", "")+"'").Output()
		dir := strings.ReplaceAll(fam, " ", "")
		n := 0
		for _, src := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if src == "" {
				continue
			}
			data, err := os.ReadFile(src)
			if err != nil {
				continue
			}
			if err := writeFile(filepath.Join(r.Root, "fonts", "files", dir, filepath.Base(src)), data, ""); err != nil {
				return nil, err
			}
			n++
		}
		if n == 0 {
			return nil, fmt.Errorf("no Homebrew cask and no installed files found for %q", fam)
		}
		f.Files = dir
	}
	entry := "\n[[font]]\nfamily = " + strconv.Quote(fam) + "\n"
	if f.Cask != "" {
		entry += "cask = " + strconv.Quote(f.Cask) + "\n"
	} else {
		entry += "files = " + strconv.Quote(f.Files) + "\n"
	}
	fh, err := os.OpenFile(r.fontsFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if _, err := fh.WriteString(entry); err != nil {
		return nil, err
	}
	r.Fonts = append(r.Fonts, f)
	return f, nil
}
