package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	scrTools screen = iota
	scrFiles
	scrDiff
	scrFonts
	scrInstall
	scrBackups
)

var (
	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	stOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stDown   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	stUp     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	stKey    = lipgloss.NewStyle().Bold(true)
	stBadge  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12")).Padding(0, 1)
)

func statusStyle(s Status) lipgloss.Style {
	switch s {
	case Synced:
		return stOK
	case RepoOnly, RepoAhead:
		return stDown
	case LocalOnly, LocalAhead:
		return stUp
	}
	return stBad
}

type toolRow struct {
	tool    *Tool
	entries []*Entry
}

func (t toolRow) pending() []*Entry {
	var out []*Entry
	for _, e := range t.entries {
		if e.Status != Synced {
			out = append(out, e)
		}
	}
	return out
}

type model struct {
	repo     *Repo
	scr      screen
	rows     []toolRow
	tc, fc   int
	fonts    []*FontRow
	ftc      int
	vp       viewport.Model
	title    string
	msg      string
	msgErr   bool
	busy     string
	w, h     int
	dirty    int
	conflict []*Entry // queue of merges waiting for the editor
	prefer   Side     // who wins what a magic merge cannot settle
	confirm  *confirm // a y/n question waiting for an answer
	backups  []backupRow
	bc       int
	spin     spinner.Model

	// install screen
	inst    InstallOpts // Tools is always non-nil here: the ticked tools
	ic      int
	pkgs    []*Pkg
	instOut string // "back" or "quit": where esc goes
}

// ── messages ───────────────────────────────────────────────────────────────
type refreshMsg struct {
	rows    []toolRow
	dirty   int
	backups []backupRow
}

// backupRow is one timestamped set of files that were replaced on this machine.
type backupRow struct {
	stamp string
	files []*Entry
}

// confirm is a question asked before something that overwrites many files.
type confirm struct {
	text string
	yes  func() (tea.Model, tea.Cmd)
}
type fontsMsg []*FontRow
type pkgsMsg []*Pkg
type installDoneMsg struct{ err error }
type doneMsg struct {
	text      string
	err       error
	conflicts []*Entry
	fonts     bool // refresh fonts afterwards
}
type editorDoneMsg struct {
	e    *Entry
	path string
	err  error
}

func runTUI(r *Repo) error {
	m := newModel(r)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// runTUIInstall opens the TUI straight on the install picker (dot install --pick).
func runTUIInstall(r *Repo, o InstallOpts) error {
	m := newModel(r)
	m.instOut = "quit"
	m.openInstall(o)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newModel(r *Repo) *model {
	return &model{repo: r, busy: "scanning…", spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(stCursor))}
}

func (m *model) Init() tea.Cmd {
	if m.scr == scrInstall {
		return tea.Batch(m.refresh(), m.loadPkgs(), m.spin.Tick)
	}
	return tea.Batch(m.refresh(), m.spin.Tick)
}

// openInstall shows the install picker; a nil o.Tools ticks every tool.
func (m *model) openInstall(o InstallOpts) {
	if o.Tools == nil {
		o.Tools = Selection{}
		for _, t := range m.repo.Tools {
			o.Tools[t.Name] = true
		}
	}
	m.inst, m.ic, m.scr, m.msg = o, 0, scrInstall, ""
}

func (m *model) loadPkgs() tea.Cmd {
	r := m.repo
	return func() tea.Msg { return pkgsMsg(r.Packages()) }
}

// installCmd runs `dot install <ticked tools>` in the foreground so brew's
// output (and any sudo prompt) is visible, then waits for enter.
func (m *model) installCmd() tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return installDoneMsg{err} }
	}
	script := `"$0" "$@"; s=$?; printf '\n\033[2mpress enter to return to dot\033[0m'; read _; exit $s`
	c := exec.Command("sh", append([]string{"-c", script, exe}, m.inst.Args()...)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return installDoneMsg{err} })
}

func (m *model) refresh() tea.Cmd {
	r := m.repo
	return func() tea.Msg {
		var rows []toolRow
		for _, t := range r.Tools {
			rows = append(rows, toolRow{tool: t, entries: r.Scan(t)})
		}
		var backups []backupRow
		for _, s := range r.Backups() {
			backups = append([]backupRow{{s, r.BackupFiles(s)}}, backups...) // newest first
		}
		return refreshMsg{rows: rows, dirty: gitDirty(r.Root), backups: backups}
	}
}

func (m *model) loadFonts() tea.Cmd {
	r := m.repo
	return func() tea.Msg { return fontsMsg(r.FontRows()) }
}

func gitDirty(root string) int {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// syncCmd does the obvious action for each entry, collecting conflicts.
func (m *model) syncCmd(entries []*Entry) tea.Cmd {
	r := m.repo
	return func() tea.Msg {
		var conflicts []*Entry
		var errs []string
		n := 0
		for _, e := range entries {
			err := r.Auto(e)
			switch {
			case errors.Is(err, ErrConflict):
				conflicts = append(conflicts, e)
			case err != nil:
				errs = append(errs, e.Rel+": "+err.Error())
			case e.Status != Synced:
				n++
			}
		}
		d := doneMsg{text: fmt.Sprintf("synced %d file(s)", n), conflicts: conflicts}
		if len(conflicts) > 0 {
			d.text += fmt.Sprintf(", %d need a manual merge", len(conflicts))
		}
		if len(errs) > 0 {
			d.err = errors.New(strings.Join(errs, "; "))
		}
		return d
	}
}

func plural(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", what)
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// magicCmd syncs entries and settles every conflict: word by word where
// possible, otherwise the preferred side wins.
func (m *model) magicCmd(entries []*Entry) tea.Cmd {
	r, prefer := m.repo, m.prefer
	return func() tea.Msg {
		var errs []string
		var n, magic, taken, whole int
		for _, e := range entries {
			f, err := r.ForceMerge(e, prefer)
			if err != nil {
				errs = append(errs, e.Rel+": "+err.Error())
				continue
			}
			n++
			magic, taken = magic+f.Magic, taken+f.Taken
			if f.Whole != "" {
				whole++
			}
		}
		text := "synced " + plural(n, "file")
		if magic > 0 {
			text += " · " + plural(magic, "conflict") + " merged word by word"
		}
		if taken > 0 {
			text += fmt.Sprintf(" · %s taken from the %s", plural(taken, "conflict"), prefer)
		}
		if whole > 0 {
			text += fmt.Sprintf(" · %s taken whole from the %s", plural(whole, "file"), prefer)
		}
		d := doneMsg{text: text}
		if len(errs) > 0 {
			d.err = errors.New(strings.Join(errs, "; "))
		}
		return d
	}
}

// toApply lists the entries whose machine copy differs from the repo's.
func toApply(entries []*Entry) []*Entry {
	var out []*Entry
	for _, e := range entries {
		if e.Status != Synced && e.Status != LocalOnly {
			out = append(out, e)
		}
	}
	return out
}

// askApply confirms, then overwrites the machine's copies with the repo's.
func (m *model) askApply(what string, entries []*Entry) (tea.Model, tea.Cmd) {
	entries = toApply(entries)
	if len(entries) == 0 {
		m.setMsg("this machine already has the repo's "+what, nil)
		return m, nil
	}
	r := m.repo
	m.confirm = &confirm{
		text: fmt.Sprintf("overwrite %s on this machine with the repo's copy?", plural(len(entries), "file")),
		yes: func() (tea.Model, tea.Cmd) {
			return m.start("applying the repo…", func() tea.Msg {
				var errs []string
				for _, e := range entries {
					if err := r.Apply(e); err != nil {
						errs = append(errs, e.Rel+": "+err.Error())
					}
				}
				d := doneMsg{text: fmt.Sprintf("applied %s — b lists the backup to undo it", plural(len(entries)-len(errs), "file"))}
				if len(errs) > 0 {
					d.err = errors.New(strings.Join(errs, "; "))
				}
				return d
			})
		},
	}
	return m, nil
}

// askRestore confirms, then puts a backup's files back on this machine.
func (m *model) askRestore(b backupRow) (tea.Model, tea.Cmd) {
	r := m.repo
	m.confirm = &confirm{
		text: fmt.Sprintf("put the %s from %s back on this machine?", plural(len(b.files), "file"), stampTime(b.stamp)),
		yes: func() (tea.Model, tea.Cmd) {
			return m.start("restoring…", func() tea.Msg {
				var errs []string
				n := 0
				for _, e := range b.files {
					same, err := r.Restore(b.stamp, e)
					switch {
					case err != nil:
						errs = append(errs, e.Rel+": "+err.Error())
					case !same:
						n++
					}
				}
				d := doneMsg{text: "restored " + plural(n, "file")}
				if n == 0 {
					d.text = "nothing to restore — this machine already has those versions"
				}
				if len(errs) > 0 {
					d.err = errors.New(strings.Join(errs, "; "))
				}
				return d
			})
		},
	}
	return m, nil
}

// stampTime renders a backup timestamp for people.
func stampTime(stamp string) string {
	t, err := time.ParseInLocation(stampLayout, stamp, time.Local)
	if err != nil {
		return stamp
	}
	return t.Format("Mon 2 Jan 2006 15:04:05")
}

func stampAge(stamp string) string {
	t, err := time.ParseInLocation(stampLayout, stamp, time.Local)
	if err != nil {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	default:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
}

func (m *model) oneCmd(label string, f func() error) tea.Cmd {
	return func() tea.Msg {
		if err := f(); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: label}
	}
}

func editorArgs(path string) []string {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		if _, err := exec.LookPath("vim"); err == nil {
			ed = "vim"
		} else {
			ed = "vi"
		}
	}
	return append(strings.Fields(ed), path)
}

// openMerge merges an entry; clean merges are saved, conflicts open $EDITOR.
func (m *model) openMerge(e *Entry) tea.Cmd {
	m.repo.Restamp()
	merged, clean, err := m.repo.Merge(e)
	if err != nil {
		return func() tea.Msg { return doneMsg{err: fmt.Errorf("%s: %w", e.Rel, err)} }
	}
	if clean {
		return m.oneCmd("merged "+e.Rel+" cleanly", func() error { return m.repo.Resolve(e, merged) })
	}
	dir, err := os.MkdirTemp("", "dot-conflict")
	if err != nil {
		return func() tea.Msg { return doneMsg{err: err} }
	}
	path := filepath.Join(dir, filepath.Base(e.Rel)) // keep the name so the editor picks a filetype
	if err := os.WriteFile(path, merged, 0o600); err != nil {
		return func() tea.Msg { return doneMsg{err: err} }
	}
	args := editorArgs(path)
	return tea.ExecProcess(exec.Command(args[0], args[1:]...), func(err error) tea.Msg {
		return editorDoneMsg{e: e, path: path, err: err}
	})
}

func (m *model) nextConflict() tea.Cmd {
	if len(m.conflict) == 0 {
		return m.refresh()
	}
	e := m.conflict[0]
	m.conflict = m.conflict[1:]
	return m.openMerge(e)
}

func (m *model) commitCmd() tea.Cmd {
	root := m.repo.Root
	return func() tea.Msg {
		if gitDirty(root) == 0 {
			return doneMsg{text: "nothing to commit"}
		}
		if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
			return doneMsg{err: fmt.Errorf("git add: %s", strings.TrimSpace(string(out)))}
		}
		out, err := exec.Command("git", "-C", root, "commit", "-q", "-m", "Sync settings").CombinedOutput()
		if err != nil {
			return doneMsg{err: fmt.Errorf("git commit: %s", strings.TrimSpace(string(out)))}
		}
		return doneMsg{text: "committed"}
	}
}

// ── update ─────────────────────────────────────────────────────────────────

func (m *model) setMsg(text string, err error) {
	m.msg, m.msgErr = text, err != nil
	if err != nil {
		m.msg = err.Error()
	}
}

func (m *model) curTool() *toolRow {
	if m.tc < len(m.rows) {
		return &m.rows[m.tc]
	}
	return nil
}

func (m *model) curEntry() *Entry {
	if t := m.curTool(); t != nil && m.fc < len(t.entries) {
		return t.entries[m.fc]
	}
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.vp.Width, m.vp.Height = msg.Width, max(1, msg.Height-4)
		return m, nil
	case refreshMsg:
		m.rows, m.dirty, m.busy, m.backups = msg.rows, msg.dirty, "", msg.backups
		m.bc = max(0, min(m.bc, len(m.backups)-1))
		if t := m.curTool(); t != nil && m.fc >= len(t.entries) {
			m.fc = max(0, len(t.entries)-1)
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case pkgsMsg:
		m.pkgs = msg
		return m, nil
	case installDoneMsg:
		if msg.err != nil {
			m.setMsg("", fmt.Errorf("install: %w", msg.err))
		} else {
			m.setMsg("install finished", nil)
		}
		m.busy = "scanning…"
		return m, tea.Batch(m.refresh(), m.loadPkgs())
	case fontsMsg:
		m.fonts, m.busy = msg, ""
		if m.ftc >= len(m.fonts) {
			m.ftc = max(0, len(m.fonts)-1)
		}
		return m, nil
	case doneMsg:
		m.busy = ""
		m.setMsg(msg.text, msg.err)
		if msg.fonts {
			return m, m.loadFonts()
		}
		m.conflict = append(m.conflict, msg.conflicts...)
		return m, m.nextConflict()
	case editorDoneMsg:
		defer os.RemoveAll(filepath.Dir(msg.path))
		data, err := os.ReadFile(msg.path)
		switch {
		case msg.err != nil:
			m.setMsg("", fmt.Errorf("editor: %w", msg.err))
		case err != nil:
			m.setMsg("", err)
		case HasConflictMarkers(data):
			m.setMsg("", fmt.Errorf("conflict markers left in %s — not saved (press m to try again)", msg.e.Rel))
		default:
			if err := m.repo.Resolve(msg.e, data); err != nil {
				m.setMsg("", err)
			} else {
				m.setMsg("merged "+msg.e.Rel, nil)
			}
		}
		return m, m.nextConflict()
	case tea.KeyMsg:
		// Keys typed or repeated faster than they're read arrive as one event
		// ("jjj"); replay them one by one so every screen sees single keys.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt})
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy != "" {
			return m, nil
		}
		if c := m.confirm; c != nil {
			m.confirm = nil
			switch msg.String() {
			case "y", "Y", "enter":
				return c.yes()
			}
			m.setMsg("cancelled", nil)
			return m, nil
		}
		switch m.scr {
		case scrTools:
			return m.keyTools(msg)
		case scrFiles:
			return m.keyFiles(msg)
		case scrDiff:
			switch msg.String() {
			case "esc", "q", "left", "h":
				m.scr = scrFiles
				return m, nil
			}
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		case scrFonts:
			return m.keyFonts(msg)
		case scrInstall:
			return m.keyInstall(msg)
		case scrBackups:
			return m.keyBackups(msg)
		}
	}
	return m, nil
}

func (m *model) start(label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.busy, m.msg = label, ""
	m.repo.Restamp()
	return m, cmd
}

func (m *model) keyTools(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.tc = max(0, m.tc-1)
	case "down", "j":
		m.tc = min(len(m.rows)-1, m.tc+1)
	case "enter", "right", "l":
		if m.curTool() != nil {
			m.scr, m.fc, m.msg = scrFiles, 0, ""
		}
	case "s":
		if t := m.curTool(); t != nil {
			return m.start("syncing "+t.tool.Name+"…", m.syncCmd(t.pending()))
		}
	case "S":
		var all []*Entry
		for _, t := range m.rows {
			all = append(all, t.pending()...)
		}
		return m.start("syncing everything…", m.syncCmd(all))
	case "w":
		if t := m.curTool(); t != nil {
			return m.start("magic-merging "+t.tool.Name+"…", m.magicCmd(t.pending()))
		}
	case "W":
		var all []*Entry
		for _, t := range m.rows {
			all = append(all, t.pending()...)
		}
		return m.start("magic-merging everything…", m.magicCmd(all))
	case "p":
		if t := m.curTool(); t != nil {
			return m.askApply(t.tool.Name+" files", t.entries)
		}
	case "P":
		var all []*Entry
		for _, t := range m.rows {
			all = append(all, t.entries...)
		}
		return m.askApply("files", all)
	case "o":
		m.prefer = RepoSide - m.prefer
		m.setMsg(fmt.Sprintf("when a magic merge cannot keep both sides, the %s now wins", m.prefer), nil)
	case "b":
		m.scr, m.msg = scrBackups, ""
	case "f":
		m.scr, m.msg = scrFonts, ""
		return m.start("checking fonts…", m.loadFonts())
	case "c":
		return m.start("committing…", m.commitCmd())
	case "i":
		m.instOut = "back"
		m.openInstall(InstallOpts{})
		return m, m.loadPkgs()
	case "r":
		return m.start("scanning…", m.refresh())
	}
	return m, nil
}

func (m *model) keyInstall(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	tools := m.repo.Tools
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		if m.instOut == "quit" {
			return m, tea.Quit
		}
		m.scr, m.msg = scrTools, ""
	case "up", "k":
		m.ic = max(0, m.ic-1)
	case "down", "j":
		m.ic = min(len(tools)-1, m.ic+1)
	case " ", "x":
		if m.ic < len(tools) {
			n := tools[m.ic].Name
			m.inst.Tools[n] = !m.inst.Tools[n]
			if !m.inst.Tools[n] {
				delete(m.inst.Tools, n)
			}
		}
	case "a":
		for _, t := range tools {
			m.inst.Tools[t.Name] = true
		}
	case "n":
		m.inst.Tools = Selection{}
	case "b":
		m.inst.NoBrew = !m.inst.NoBrew
	case "f":
		m.inst.NoFonts = !m.inst.NoFonts
	case "enter":
		if len(m.inst.Tools) == 0 {
			m.setMsg("", errors.New("no tools ticked — space to tick one, a for all"))
			return m, nil
		}
		m.msg = ""
		return m, m.installCmd()
	}
	return m, nil
}

func (m *model) keyBackups(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		m.scr, m.msg = scrTools, ""
	case "up", "k":
		m.bc = max(0, m.bc-1)
	case "down", "j":
		m.bc = max(0, min(len(m.backups)-1, m.bc+1))
	case "enter", "r":
		if m.bc < len(m.backups) {
			return m.askRestore(m.backups[m.bc])
		}
	}
	return m, nil
}

func (m *model) keyFiles(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t, e := m.curTool(), m.curEntry()
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		m.scr, m.msg = scrTools, ""
	case "up", "k":
		m.fc = max(0, m.fc-1)
	case "down", "j":
		if t != nil {
			m.fc = min(len(t.entries)-1, m.fc+1)
		}
	case "a":
		if t != nil {
			return m.start("syncing "+t.tool.Name+"…", m.syncCmd(t.pending()))
		}
	case "r":
		return m.start("scanning…", m.refresh())
	}
	if e == nil {
		return m, nil
	}
	switch k.String() {
	case "enter", "d", "right", "l":
		m.title = e.Tool.Name + " · " + e.Rel
		m.vp.SetContent(m.repo.Diff(e))
		m.vp.GotoTop()
		m.scr = scrDiff
	case "s":
		return m.start("syncing…", m.syncCmd([]*Entry{e}))
	case "p":
		if e.Status == LocalOnly {
			m.setMsg("", errors.New("not in the repo — use i to import it"))
			return m, nil
		}
		return m.start("applying…", m.oneCmd("applied repo → "+m.repo.Tilde(m.repo.LocalPath(e)), func() error { return m.repo.Apply(e) }))
	case "i":
		if e.Status == RepoOnly {
			m.setMsg("", errors.New("not on this machine — use p to apply it"))
			return m, nil
		}
		return m.start("importing…", m.oneCmd("imported "+e.Rel+" into repo", func() error { return m.repo.Import(e) }))
	case "m":
		if e.Status == RepoOnly || e.Status == LocalOnly {
			m.setMsg("", errors.New("only one side exists — nothing to merge"))
			return m, nil
		}
		return m, m.openMerge(e)
	case "w":
		return m.start("magic-merging…", m.magicCmd([]*Entry{e}))
	}
	return m, nil
}

func (m *model) keyFonts(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		m.scr, m.msg = scrTools, ""
		return m, nil
	case "up", "k":
		m.ftc = max(0, m.ftc-1)
		return m, nil
	case "down", "j":
		m.ftc = min(len(m.fonts)-1, m.ftc+1)
		return m, nil
	case "r":
		return m.start("checking fonts…", m.loadFonts())
	case "I":
		var todo []*Font
		for _, f := range m.fonts {
			if f.NeedsInstall() {
				todo = append(todo, f.Font)
			}
		}
		return m.start("installing fonts…", m.installFonts(todo))
	}
	if m.ftc >= len(m.fonts) {
		return m, nil
	}
	fr := m.fonts[m.ftc]
	switch k.String() {
	case "i", "enter":
		if fr.Font == nil {
			m.setMsg("", errors.New("not in fonts.toml yet — press a to add it"))
			return m, nil
		}
		if !fr.NeedsInstall() {
			m.setMsg("already available", nil)
			return m, nil
		}
		return m.start("installing "+fr.Family+"…", m.installFonts([]*Font{fr.Font}))
	case "a":
		if fr.Font != nil {
			m.setMsg("already in fonts.toml", nil)
			return m, nil
		}
		fam := fr.Family
		return m.start("looking up "+fam+"…", func() tea.Msg {
			f, err := m.repo.AddFont(fam)
			if err != nil {
				return doneMsg{err: err, fonts: true}
			}
			return doneMsg{text: "added " + fam + " (" + f.Source() + ")", fonts: true}
		})
	}
	return m, nil
}

func (m *model) installFonts(fonts []*Font) tea.Cmd {
	r := m.repo
	return func() tea.Msg {
		if len(fonts) == 0 {
			return doneMsg{text: "all fonts available", fonts: true}
		}
		var errs []string
		for _, f := range fonts {
			if out, err := r.InstallFont(f); err != nil {
				errs = append(errs, f.Family+": "+lastLine(out, err))
			}
		}
		if len(errs) > 0 {
			return doneMsg{err: errors.New(strings.Join(errs, "; ")), fonts: true}
		}
		return doneMsg{text: fmt.Sprintf("installed %d font(s)", len(fonts)), fonts: true}
	}
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// ── view ───────────────────────────────────────────────────────────────────

func (m *model) View() string {
	var b strings.Builder
	head := stBadge.Render("dot") + stDim.Render("  "+m.repo.Tilde(m.repo.Root))
	if m.dirty > 0 {
		head += stUp.Render("  ● "+plural(m.dirty, "uncommitted change")) + stDim.Render(" (c commits)")
	}
	b.WriteString(head + "\n\n")

	var body, help string
	switch m.scr {
	case scrTools:
		body = m.viewTools()
		help = m.keys("↑↓", "move", "enter", "files", "s/S", "sync tool/all", "w/W", "magic merge tool/all", "p/P", "repo → machine tool/all") + "\n" +
			m.keys("o", "conflict winner", "b", "backups", "i", "install", "f", "fonts", "c", "commit", "r", "rescan", "q", "quit")
	case scrFiles:
		body = m.viewFiles()
		help = m.keys("enter", "diff", "s", "sync", "p", "repo → machine", "i", "machine → repo", "m", "merge in editor", "w", "magic merge", "a", "sync all", "esc", "back")
	case scrDiff:
		b.Reset()
		b.WriteString(stBadge.Render("diff") + "  " + stTitle.Render(m.title) + "  " + stBad.Render("- repo") + "  " + stOK.Render("+ machine") + "\n\n")
		b.WriteString(m.vp.View() + "\n")
		b.WriteString(m.keys("↑↓/pgup/pgdn", "scroll", "esc", "back") + stDim.Render(fmt.Sprintf("   %3.0f%%", m.vp.ScrollPercent()*100)))
		return b.String()
	case scrFonts:
		body = m.viewFonts()
		help = m.keys("i", "install", "I", "install missing", "a", "add to fonts.toml", "r", "recheck", "esc", "back")
	case scrInstall:
		body = m.viewInstall()
		help = m.keys("space", "tick", "a", "all", "n", "none", "b", "packages", "f", "fonts", "enter", "install", "esc", "back")
	case scrBackups:
		body = m.viewBackups()
		help = m.keys("↑↓", "move", "enter", "restore", "esc", "back")
	}
	b.WriteString(body)
	b.WriteString("\n")
	switch {
	case m.busy != "":
		b.WriteString(m.spin.View() + " " + stDim.Render(m.busy))
	case m.confirm != nil:
		b.WriteString(stUp.Bold(true).Render("? "+m.confirm.text) + "  " + m.keys("y", "yes", "n", "no"))
	case m.msgErr:
		b.WriteString(stBad.Render("✗ " + m.msg))
	case m.msg != "":
		b.WriteString(stOK.Render("✓ " + m.msg))
	}
	b.WriteString("\n" + help)
	return b.String()
}

// keys renders key hints, wrapped to the window width.
func (m *model) keys(pairs ...string) string {
	sep := stDim.Render("  ·  ")
	var lines []string
	line := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		part := stKey.Render(pairs[i]) + " " + stDim.Render(pairs[i+1])
		switch {
		case line == "":
			line = part
		case m.w > 0 && lipgloss.Width(line+sep+part) > m.w:
			lines = append(lines, line)
			line = part
		default:
			line += sep + part
		}
	}
	return strings.Join(append(lines, line), "\n")
}

func (m *model) viewBackups() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("backups") + stDim.Render("  files dot replaced on this machine, newest first") + "\n\n")
	if len(m.backups) == 0 {
		return b.String() + stDim.Render("  none yet — dot saves a file here before overwriting it") + "\n"
	}
	avail := max(3, (m.h-10)/2)
	lo := max(0, min(m.bc-avail+1, len(m.backups)-avail))
	for i := lo; i < min(len(m.backups), lo+avail); i++ {
		bk := m.backups[i]
		when := lipgloss.NewStyle().Width(26).Bold(i == m.bc).Render(stampTime(bk.stamp))
		n := lipgloss.NewStyle().Width(10).Render(plural(len(bk.files), "file"))
		fmt.Fprintf(&b, "%s%s %s %s\n", cursor(i == m.bc), when, n, stDim.Render(stampAge(bk.stamp)))
	}
	if len(m.backups) > avail {
		b.WriteString(stDim.Render(fmt.Sprintf("  … %d/%d", m.bc+1, len(m.backups))) + "\n")
	}
	bk := m.backups[m.bc]
	b.WriteString("\n" + stDim.Render("  restoring "+bk.stamp+" brings back:") + "\n")
	for i, e := range bk.files {
		if i == avail && len(bk.files) > avail+1 {
			b.WriteString(stDim.Render(fmt.Sprintf("    … and %d more", len(bk.files)-i)) + "\n")
			break
		}
		b.WriteString("    " + showPath(e.Rel) + "\n")
	}
	return b.String()
}

// showPath renders ~/dir/name with the directory dimmed.
func showPath(rel string) string {
	dir, name := filepath.Split(rel)
	return stDim.Render("~/"+dir) + name
}

func cursor(on bool) string {
	if on {
		return stCursor.Render("▸ ")
	}
	return "  "
}

func (m *model) viewTools() string {
	if len(m.rows) == 0 {
		if m.busy != "" {
			return "\n"
		}
		return stDim.Render("  no tools in tools.toml") + "\n"
	}
	var b strings.Builder
	nameW, descW := 0, 0
	var tApply, tImp, tMerge, tOK int
	for _, t := range m.rows {
		nameW, descW = max(nameW, len(t.tool.Name)), max(descW, lipgloss.Width(t.tool.Description))
		for _, e := range t.entries {
			switch e.Status {
			case Synced:
				tOK++
			case RepoOnly, RepoAhead:
				tApply++
			case LocalOnly, LocalAhead:
				tImp++
			case Diverged:
				tMerge++
			}
		}
	}
	total := []string{stOK.Render("✓ " + plural(tOK, "file") + " in sync")}
	if tApply > 0 {
		total = append(total, stDown.Render(fmt.Sprintf("↓ %d to apply", tApply)))
	}
	if tImp > 0 {
		total = append(total, stUp.Render(fmt.Sprintf("↑ %d to import", tImp)))
	}
	if tMerge > 0 {
		total = append(total, stBad.Render(fmt.Sprintf("⇅ %d to merge", tMerge)))
	}
	b.WriteString("  " + strings.Join(total, stDim.Render("  ·  ")) + "\n\n")
	for i, t := range m.rows {
		var apply, imp, merge, ok int
		for _, e := range t.entries {
			switch e.Status {
			case Synced:
				ok++
			case RepoOnly, RepoAhead:
				apply++
			case LocalOnly, LocalAhead:
				imp++
			case Diverged:
				merge++
			}
		}
		var sum []string
		if len(t.entries) == 0 {
			sum = append(sum, stDim.Render("nothing here yet"))
		} else if apply+imp+merge == 0 {
			sum = append(sum, stOK.Render(fmt.Sprintf("✓ %d in sync", ok)))
		}
		if apply > 0 {
			sum = append(sum, stDown.Render(fmt.Sprintf("↓ %d to apply", apply)))
		}
		if imp > 0 {
			sum = append(sum, stUp.Render(fmt.Sprintf("↑ %d to import", imp)))
		}
		if merge > 0 {
			sum = append(sum, stBad.Render(fmt.Sprintf("⇅ %d to merge", merge)))
		}
		nameSt := lipgloss.NewStyle().Width(nameW + 1)
		if i == m.tc {
			nameSt = nameSt.Inherit(stCursor)
		}
		name := nameSt.Render(t.tool.Name)
		desc := lipgloss.NewStyle().Width(descW + 1).Render(stDim.Render(t.tool.Description))
		fmt.Fprintf(&b, "%s%s %s %s\n", cursor(i == m.tc), name, desc, strings.Join(sum, "  "))
	}
	b.WriteString("\n" + stDim.Render("  ↓ apply = repo → this machine   ↑ import = this machine → repo   ⇅ merge both") + "\n")
	b.WriteString(stDim.Render("  magic merge keeps both sides word by word; where it cannot, the ") + stKey.Render(m.prefer.String()) + stDim.Render(" wins") + "\n")
	return b.String()
}

func (m *model) viewFiles() string {
	t := m.curTool()
	if t == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(stTitle.Render(t.tool.Name) + stDim.Render("  "+t.tool.Description) + "\n\n")
	if len(t.entries) == 0 {
		b.WriteString(stDim.Render("  No files yet. Paths tracked:") + "\n")
		for _, p := range t.tool.Paths {
			b.WriteString(stDim.Render("    ~/"+p) + "\n")
		}
		return b.String()
	}
	avail := max(3, m.h-12)
	lo := 0
	if m.fc >= avail {
		lo = m.fc - avail + 1
	}
	hi := min(len(t.entries), lo+avail)
	for i := lo; i < hi; i++ {
		e := t.entries[i]
		st := statusStyle(e.Status).Width(16).Render(e.Status.Symbol() + " " + e.Status.String())
		fmt.Fprintf(&b, "%s%s %s\n", cursor(i == m.fc), st, showPath(e.Rel))
	}
	if hi < len(t.entries) || lo > 0 {
		b.WriteString(stDim.Render(fmt.Sprintf("  … %d/%d", m.fc+1, len(t.entries))) + "\n")
	}
	if e := m.curEntry(); e != nil {
		hint := ""
		switch e.Status {
		case RepoOnly, RepoAhead:
			hint = "s copies the repo's version onto this machine"
		case LocalOnly, LocalAhead:
			hint = "s copies this machine's version into the repo"
		case Diverged:
			hint = fmt.Sprintf("changed on both sides — s merges if clean · w magic merge (%s wins ties) · m editor", m.prefer)
		}
		b.WriteString("\n" + stDim.Render("  "+hint) + "\n")
	}
	return b.String()
}

func (m *model) viewFonts() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("fonts") + stDim.Render("  listed in fonts/fonts.toml or referenced by a config") + "\n\n")
	if len(m.fonts) == 0 {
		b.WriteString(stDim.Render("  none") + "\n")
	}
	for i, f := range m.fonts {
		var state string
		switch {
		case f.Installed:
			state = stOK.Render("✓ installed")
		case f.Font != nil && f.Font.Builtin:
			state = stOK.Render("✓ bundled")
		default:
			state = stBad.Render("✗ missing")
		}
		src := stUp.Render("not in fonts.toml")
		if f.Font != nil {
			src = stDim.Render(f.Font.Source())
		}
		used := ""
		if len(f.UsedBy) > 0 {
			used = stDim.Render("used by " + strings.Join(f.UsedBy, ", "))
		}
		fmt.Fprintf(&b, "%s%s %s %s %s\n", cursor(i == m.ftc),
			lipgloss.NewStyle().Width(30).Render(f.Family),
			lipgloss.NewStyle().Width(13).Render(state),
			lipgloss.NewStyle().Width(36).Render(src), used)
	}
	return b.String()
}

func checkbox(on bool) string {
	if on {
		return stOK.Render("[x]")
	}
	return stDim.Render("[ ]")
}

func (m *model) viewInstall() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("install") + stDim.Render("  missing packages, then fonts, then the repo's configs — for the ticked tools") + "\n\n")
	missing := map[string][]string{}
	for _, p := range m.pkgs {
		if !p.Installed {
			for _, t := range p.UsedBy {
				missing[t] = append(missing[t], p.Name)
			}
		}
	}
	entries := map[string][]*Entry{}
	for _, r := range m.rows {
		entries[r.tool.Name] = r.entries
	}
	for i, t := range m.repo.Tools {
		var sum []string
		switch {
		case m.pkgs == nil && (len(t.Brew)+len(t.Cask) > 0):
			sum = append(sum, stDim.Render("checking packages…"))
		case len(missing[t.Name]) > 0:
			sum = append(sum, stBad.Render("✗ needs "+strings.Join(missing[t.Name], ", ")))
		}
		apply := 0
		for _, e := range entries[t.Name] {
			if e.Status == RepoOnly || e.Status == RepoAhead || e.Status == Diverged {
				apply++
			}
		}
		if apply > 0 {
			sum = append(sum, stDown.Render(fmt.Sprintf("↓ %d config file(s) to apply", apply)))
		}
		if len(sum) == 0 {
			sum = append(sum, stOK.Render("✓ nothing to do"))
		}
		name := lipgloss.NewStyle().Width(11).Bold(i == m.ic).Render(t.Name)
		fmt.Fprintf(&b, "%s%s %s %s\n", cursor(i == m.ic), checkbox(m.inst.Tools[t.Name]), name, strings.Join(sum, "  "))
	}
	fmt.Fprintf(&b, "\n  %s packages (brew)   %s fonts   %s\n",
		checkbox(!m.inst.NoBrew), checkbox(!m.inst.NoFonts), stDim.Render("configs: brought down from the repo, local changes kept"))
	return b.String()
}
