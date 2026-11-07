// dot — manage dotfiles for a set of supported tools.
//
//	dot            interactive TUI
//	dot status     print what differs between the repo and this machine
//	dot sync       apply/import/merge everything that does not conflict
//	dot install    new machine: Brewfile, fonts, then apply the repo
//	dot fonts      list fonts used by the configs
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	repo, err := LoadRepo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dot:", err)
		os.Exit(1)
	}
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "":
		err = runTUI(repo)
	case "status", "st":
		cliStatus(repo)
	case "sync":
		cliSync(repo, false)
	case "install":
		cliInstall(repo, args[1:])
	case "fonts":
		cliFonts(repo)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "dot: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dot:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`dot — dotfiles for ghostty, herdr, vim and friends

  dot              open the TUI
  dot status       show what differs between the repo and this machine
  dot sync         apply/import/merge everything that has no conflicts
  dot install      new machine: brew bundle, install fonts, apply the repo
                   (--no-brew, --no-fonts)
  dot fonts        list fonts referenced by the configs

Supported tools are declared in tools.toml; fonts in fonts/fonts.toml.
Before overwriting a file on this machine, dot saves it to
~/.local/state/dotfiles/backup/<timestamp>/.
`)
}

func cliStatus(r *Repo) {
	for _, t := range r.Tools {
		fmt.Printf("%s\n", stTitle.Render(t.Name))
		entries := r.Scan(t)
		if len(entries) == 0 {
			fmt.Println(stDim.Render("  (nothing found)"))
		}
		for _, e := range entries {
			fmt.Printf("  %s ~/%s\n", statusStyle(e.Status).Width(16).Render(e.Status.Symbol()+" "+e.Status.String()), e.Rel)
		}
	}
}

// cliSync runs the non-interactive part of a sync. With install=true it never
// imports machine-only files; it only brings the repo onto the machine.
func cliSync(r *Repo, install bool) {
	var conflicts, kept []string
	for _, t := range r.Tools {
		for _, e := range r.Scan(t) {
			if e.Status == Synced {
				continue
			}
			if install && (e.Status == LocalOnly || e.Status == LocalAhead) {
				kept = append(kept, e.Rel)
				continue
			}
			err := r.Auto(e)
			switch {
			case errors.Is(err, ErrConflict), errors.Is(err, ErrBinary):
				conflicts = append(conflicts, e.Rel)
			case err != nil:
				fmt.Printf("  %s ~/%s: %v\n", stBad.Render("✗"), e.Rel, err)
			default:
				fmt.Printf("  %s ~/%s %s\n", stOK.Render("✓"), e.Rel, stDim.Render("("+e.Status.String()+")"))
			}
		}
	}
	for _, k := range kept {
		fmt.Printf("  %s ~/%s %s\n", stUp.Render("↑"), k, stDim.Render("only on this machine — import it from the TUI if wanted"))
	}
	for _, c := range conflicts {
		fmt.Printf("  %s ~/%s %s\n", stBad.Render("⇅"), c, stDim.Render("needs a manual merge — run dot"))
	}
	if len(conflicts) == 0 && len(kept) == 0 {
		fmt.Println(stOK.Render("everything in sync"))
	}
}

func cliFonts(r *Repo) {
	for _, f := range r.FontRows() {
		state := stBad.Render("✗ missing  ")
		if f.Installed {
			state = stOK.Render("✓ installed")
		} else if f.Font != nil && f.Font.Builtin {
			state = stOK.Render("✓ bundled  ")
		}
		src := stUp.Render("not in fonts.toml")
		if f.Font != nil {
			src = f.Font.Source()
		}
		used := ""
		if len(f.UsedBy) > 0 {
			used = "  used by " + strings.Join(f.UsedBy, ", ")
		}
		fmt.Printf("%s  %-30s %s\n", state, f.Family, stDim.Render(src+used))
	}
}

func cliInstall(r *Repo, flags []string) {
	noBrew, noFonts := false, false
	for _, f := range flags {
		switch f {
		case "--no-brew":
			noBrew = true
		case "--no-fonts":
			noFonts = true
		}
	}
	if !noBrew {
		brewfile := filepath.Join(r.Root, "Brewfile")
		if _, err := exec.LookPath("brew"); err != nil {
			fmt.Println(stUp.Render("! Homebrew not found — skipping Brewfile"))
		} else if _, err := os.Stat(brewfile); err == nil {
			fmt.Println(stTitle.Render("==> Brewfile"))
			if exec.Command("brew", "bundle", "check", "--file", brewfile).Run() == nil {
				fmt.Println(stOK.Render("  ✓ everything installed"))
			} else {
				c := exec.Command("brew", "bundle", "install", "--no-upgrade", "--file", brewfile)
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				if err := c.Run(); err != nil {
					fmt.Println(stUp.Render("  ! some entries failed (apps installed outside Homebrew are fine to ignore)"))
				}
			}
		}
	}
	if !noFonts {
		fmt.Println(stTitle.Render("==> fonts"))
		for _, f := range r.FontRows() {
			switch {
			case f.Font == nil:
				fmt.Printf("  %s %s %s\n", stUp.Render("!"), f.Family, stDim.Render("is used but not in fonts.toml"))
			case !f.NeedsInstall():
				fmt.Printf("  %s %s\n", stOK.Render("✓"), f.Family)
			default:
				if out, err := r.InstallFont(f.Font); err != nil {
					fmt.Printf("  %s %s: %s\n", stBad.Render("✗"), f.Family, lastLine(out, err))
				} else {
					fmt.Printf("  %s %s %s\n", stOK.Render("✓"), f.Family, stDim.Render("installed"))
				}
			}
		}
	}
	fmt.Println(stTitle.Render("==> configs"))
	cliSync(r, true)
}
