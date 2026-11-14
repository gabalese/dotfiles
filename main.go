// dot — manage dotfiles for a set of supported tools.
//
//	dot            interactive TUI
//	dot status     print what differs between the repo and this machine
//	dot sync       apply/import/merge everything that does not conflict
//	dot merge      like sync, but settles conflicts itself (one side wins)
//	dot apply      overwrite this machine's files with the repo's
//	dot restore    bring back files a previous run replaced on this machine
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
	case "merge":
		err = cliMerge(repo, args[1:])
	case "apply":
		err = cliApply(repo, args[1:])
	case "restore":
		err = cliRestore(repo, args[1:])
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
  dot merge [--prefer machine|repo]
                   sync everything, conflicts included: conflicting lines are
                   merged word by word, and where both sides changed the same
                   words the preferred side wins (default: machine)
  dot apply [tool…]
                   make this machine match the repo: every tracked file that
                   differs is overwritten with the repo's copy (files that are
                   only on this machine are left alone)
  dot restore      list the backups of replaced machine files, by timestamp
  dot restore <timestamp>|latest
                   put the files of that backup back on this machine
  dot install [tool…]
                   install missing packages and fonts, apply the repo — for the
                   named tools, or all of them (--skip a,b  --no-brew  --no-fonts)
  dot install --pick
                   choose the tools in the TUI
  dot packages     list Homebrew packages the tools need, and which are missing
  dot fonts        list fonts referenced by the configs

Supported tools and their packages are declared in tools.toml; fonts in fonts/fonts.toml.
Before overwriting a file on this machine, dot saves it to
~/.local/state/dotfiles/backup/<timestamp>/ — dot restore brings it back.
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

// cliMerge syncs everything without asking: conflicts are merged word by
// word, and what still conflicts is taken from the preferred side.
func cliMerge(r *Repo, args []string) error {
	prefer := Machine
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--prefer" && i+1 < len(args):
			i++
			a = "--prefer=" + args[i]
			fallthrough
		case strings.HasPrefix(a, "--prefer="):
			switch strings.TrimPrefix(a, "--prefer=") {
			case "machine", "local":
				prefer = Machine
			case "repo":
				prefer = RepoSide
			default:
				return fmt.Errorf("--prefer takes machine or repo")
			}
		default:
			return fmt.Errorf("unknown argument %s", a)
		}
	}
	changed, overridden, failed := 0, 0, 0
	for _, t := range r.Tools {
		for _, e := range r.Scan(t) {
			if e.Status == Synced {
				continue
			}
			f, err := r.ForceMerge(e, prefer)
			var notes []string
			if f.Magic > 0 {
				notes = append(notes, fmt.Sprintf("%d conflict(s) merged word by word", f.Magic))
			}
			if f.Taken > 0 {
				notes = append(notes, fmt.Sprintf("%d conflict(s) taken from the %s copy", f.Taken, prefer))
			}
			if f.Whole != "" {
				notes = append(notes, fmt.Sprintf("%s: whole file taken from the %s copy", f.Whole, prefer))
			}
			if len(notes) == 0 {
				notes = []string{e.Status.String()}
			}
			note := stDim.Render("(" + strings.Join(notes, ", ") + ")")
			switch {
			case err != nil:
				failed++
				fmt.Printf("  %s ~/%s: %v\n", stBad.Render("✗"), e.Rel, err)
			case f.Taken > 0 || f.Whole != "":
				changed++
				overridden++
				fmt.Printf("  %s ~/%s %s\n", stUp.Render("⇅"), e.Rel, note)
			default:
				changed++
				fmt.Printf("  %s ~/%s %s\n", stOK.Render("✓"), e.Rel, note)
			}
		}
	}
	if overridden > 0 {
		lost := "the repo's side of those is in git: git diff"
		if prefer == RepoSide {
			lost = "the machine's side of those is in the backup below"
		}
		fmt.Println(stDim.Render(fmt.Sprintf("%d file(s) had conflicts settled for the %s — %s", overridden, prefer, lost)))
	}
	if _, err := os.Stat(r.BackupDir()); err == nil {
		fmt.Println(stDim.Render("replaced files saved — undo with: dot restore " + r.stamp))
	}
	switch {
	case failed > 0:
		return fmt.Errorf("%d file(s) could not be synced", failed)
	case changed == 0:
		fmt.Println(stOK.Render("everything in sync"))
	}
	return nil
}

// cliApply overwrites the machine's copy of every tracked file with the
// repo's. Nothing is merged or imported.
func cliApply(r *Repo, args []string) error {
	var sel Selection
	for _, a := range args {
		if !slices.ContainsFunc(r.Tools, func(t *Tool) bool { return t.Name == a }) {
			return fmt.Errorf("unknown tool %q (see tools.toml)", a)
		}
		if sel == nil {
			sel = Selection{}
		}
		sel[a] = true
	}
	applied, kept, failed := 0, 0, 0
	for _, t := range r.Tools {
		if !sel.Has(t.Name) {
			continue
		}
		for _, e := range r.Scan(t) {
			switch e.Status {
			case Synced:
				continue
			case LocalOnly:
				kept++
				continue
			}
			if err := r.Apply(e); err != nil {
				failed++
				fmt.Printf("  %s ~/%s: %v\n", stBad.Render("✗"), e.Rel, err)
				continue
			}
			applied++
			fmt.Printf("  %s ~/%s %s\n", stOK.Render("↓"), e.Rel, stDim.Render("(was: "+e.Status.String()+")"))
		}
	}
	if kept > 0 {
		fmt.Println(stDim.Render(fmt.Sprintf("%d file(s) only on this machine left alone", kept)))
	}
	if _, err := os.Stat(r.BackupDir()); err == nil {
		fmt.Println(stDim.Render("replaced files saved — undo with: dot restore " + r.stamp))
	}
	switch {
	case failed > 0:
		return fmt.Errorf("%d file(s) could not be applied", failed)
	case applied == 0:
		fmt.Println(stOK.Render("machine already matches the repo"))
	}
	return nil
}

// cliRestore lists the backups, or puts one back on the machine. The files
// it replaces are backed up in turn, so a restore can itself be undone.
func cliRestore(r *Repo, args []string) error {
	stamps := r.Backups()
	if len(args) == 0 {
		if len(stamps) == 0 {
			fmt.Println(stDim.Render("no backups yet"))
			return nil
		}
		for _, s := range stamps {
			files := r.BackupFiles(s)
			fmt.Printf("%s  %s\n", stTitle.Render(s), stDim.Render(fmt.Sprintf("%d file(s)", len(files))))
			for _, e := range files {
				fmt.Printf("  ~/%s\n", e.Rel)
			}
		}
		fmt.Println(stDim.Render("restore one with: dot restore <timestamp>   (or: latest)"))
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("restore takes one timestamp")
	}
	stamp := args[0]
	if stamp == "latest" {
		if len(stamps) == 0 {
			return fmt.Errorf("no backups yet")
		}
		stamp = stamps[len(stamps)-1]
	}
	if !slices.Contains(stamps, stamp) {
		return fmt.Errorf("no backup %q — run dot restore to list them", stamp)
	}
	failed := 0
	for _, e := range r.BackupFiles(stamp) {
		same, err := r.Restore(stamp, e)
		switch {
		case err != nil:
			failed++
			fmt.Printf("  %s ~/%s: %v\n", stBad.Render("✗"), e.Rel, err)
		case same:
			fmt.Printf("  %s ~/%s %s\n", stOK.Render("✓"), e.Rel, stDim.Render("(already that version)"))
		default:
			fmt.Printf("  %s ~/%s %s\n", stOK.Render("↺"), e.Rel, stDim.Render("(restored from "+stamp+")"))
		}
	}
	if _, err := os.Stat(r.BackupDir()); err == nil {
		fmt.Println(stDim.Render("replaced files saved — undo with: dot restore " + r.stamp))
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) could not be restored", failed)
	}
	return nil
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
