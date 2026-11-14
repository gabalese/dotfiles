// dot — manage dotfiles for a set of supported tools.
//
//	dot            interactive TUI
//	dot status     print what differs between the repo and this machine
//	dot sync       apply/import/merge everything that does not conflict
//	dot install    new machine: missing packages, fonts, then apply the repo
//	dot packages   list Homebrew packages the tools need
//	dot fonts      list fonts used by the configs
package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
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
		cliSync(repo, false, nil)
	case "install":
		err = cliInstall(repo, args[1:])
	case "fonts":
		cliFonts(repo)
	case "packages", "pkgs":
		cliPackages(repo)
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
  dot install [tool…]
                   install missing packages and fonts, apply the repo — for the
                   named tools, or all of them (--skip a,b  --no-brew  --no-fonts)
  dot install --pick
                   choose the tools in the TUI
  dot packages     list Homebrew packages the tools need, and which are missing
  dot fonts        list fonts referenced by the configs

Supported tools and their packages are declared in tools.toml; fonts in fonts/fonts.toml.
Before overwriting a file on this machine, dot saves it to
~/.local/state/dotfiles/backup/<timestamp>/.
`)
}

func cliStatus(r *Repo) {
	missing := map[string][]*Pkg{}
	for _, p := range r.Packages() {
		if !p.Installed {
			for _, t := range p.UsedBy {
				missing[t] = append(missing[t], p)
			}
		}
	}
	for _, t := range r.Tools {
		fmt.Printf("%s\n", stTitle.Render(t.Name))
		for _, p := range missing[t.Name] {
			fmt.Printf("  %s %s %s\n", stBad.Width(16).Render("✗ not installed"), p.Kind()+" "+p.Name, stDim.Render("— dot install"))
		}
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
func cliSync(r *Repo, install bool, sel Selection) {
	var conflicts, kept []string
	for _, t := range r.Tools {
		if !sel.Has(t.Name) {
			continue
		}
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

func cliPackages(r *Repo) {
	for _, p := range r.Packages() {
		state := stBad.Render("✗ missing  ")
		if p.Installed {
			state = stOK.Render("✓ installed")
		}
		fmt.Printf("%s  %-4s %-32s %s\n", state, p.Kind(), p.Name, stDim.Render("used by "+strings.Join(p.UsedBy, ", ")))
	}
}

// installPackages brew-installs every declared package this machine lacks.
// Anything already present — via Homebrew or not — is left alone.
func installPackages(r *Repo, sel Selection) {
	var missing []*Pkg
	for _, p := range r.Packages() {
		if !p.Installed && sel.Has(p.UsedBy...) {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		fmt.Println(stOK.Render("  ✓ everything installed"))
		return
	}
	if !hasBrew() {
		fmt.Println(stUp.Render("  ! Homebrew not found — skipping packages"))
		return
	}
	for _, p := range missing {
		fmt.Printf("  %s %s %s\n", stUp.Render("↓"), p.Name, stDim.Render("("+p.Kind()+", for "+strings.Join(p.UsedBy, ", ")+")"))
		c := p.InstallCmd()
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			fmt.Printf("  %s %s: %v\n", stBad.Render("✗"), p.Name, err)
		}
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

// InstallOpts says what `dot install` does, and for which tools.
type InstallOpts struct {
	Tools   Selection
	NoBrew  bool
	NoFonts bool
	Pulled  []string // "zsh (required by pure)": tools added through `requires`
}

// Args renders the options back into `dot install` arguments.
func (o InstallOpts) Args() []string {
	args := []string{"install"}
	if o.NoBrew {
		args = append(args, "--no-brew")
	}
	if o.NoFonts {
		args = append(args, "--no-fonts")
	}
	for n := range o.Tools {
		args = append(args, n)
	}
	sort.Strings(args[1:])
	return args
}

func parseInstall(r *Repo, args []string) (o InstallOpts, pick bool, err error) {
	var only, skip []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--no-brew":
			o.NoBrew = true
		case a == "--no-fonts":
			o.NoFonts = true
		case a == "--pick", a == "-p":
			pick = true
		case a == "--skip" && i+1 < len(args):
			i++
			skip = append(skip, strings.Split(args[i], ",")...)
		case strings.HasPrefix(a, "--skip="):
			skip = append(skip, strings.Split(strings.TrimPrefix(a, "--skip="), ",")...)
		case strings.HasPrefix(a, "-"):
			return o, false, fmt.Errorf("unknown flag %s", a)
		default:
			only = append(only, a)
		}
	}
	known := map[string]bool{}
	for _, t := range r.Tools {
		known[t.Name] = true
	}
	for _, n := range append(append([]string{}, only...), skip...) {
		if n != "" && !known[n] {
			return o, false, fmt.Errorf("unknown tool %q (see tools.toml)", n)
		}
	}
	if len(only) > 0 || len(skip) > 0 {
		o.Tools = Selection{}
		for _, t := range r.Tools {
			if len(only) == 0 || slices.Contains(only, t.Name) {
				o.Tools[t.Name] = true
			}
		}
		// bring along what the selected tools require; an explicit --skip still wins
		for changed := true; changed; {
			changed = false
			for _, t := range r.Tools {
				if !o.Tools[t.Name] {
					continue
				}
				for _, n := range t.Requires {
					if !o.Tools[n] && !slices.Contains(skip, n) {
						o.Tools[n], changed = true, true
						o.Pulled = append(o.Pulled, n+" (required by "+t.Name+")")
					}
				}
			}
		}
		for _, n := range skip {
			delete(o.Tools, n)
		}
	}
	return o, pick, nil
}

func cliInstall(r *Repo, args []string) error {
	o, pick, err := parseInstall(r, args)
	if err != nil {
		return err
	}
	if pick {
		return runTUIInstall(r, o)
	}
	runInstall(r, o)
	return nil
}

func runInstall(r *Repo, o InstallOpts) {
	if o.Tools != nil {
		names := make([]string, 0, len(o.Tools))
		for n := range o.Tools {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println(stUp.Render("no tools selected"))
			return
		}
		fmt.Println(stDim.Render("tools: " + strings.Join(names, ", ")))
		if len(o.Pulled) > 0 {
			fmt.Println(stDim.Render("added: " + strings.Join(o.Pulled, ", ")))
		}
	}
	if !o.NoBrew {
		fmt.Println(stTitle.Render("==> packages"))
		installPackages(r, o.Tools)
	}
	if !o.NoFonts {
		fmt.Println(stTitle.Render("==> fonts"))
		shown := 0
		for _, f := range r.FontRows() {
			// fonts.toml entries no config uses only come with a full install
			if !o.Tools.Has(f.UsedBy...) {
				continue
			}
			shown++
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
		if shown == 0 {
			fmt.Println(stDim.Render("  (none used by these tools)"))
		}
	}
	fmt.Println(stTitle.Render("==> configs"))
	cliSync(r, true, o.Tools)
}
