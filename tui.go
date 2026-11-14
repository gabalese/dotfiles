package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// install screen
	inst    InstallOpts // Tools is always non-nil here: the ticked tools
	ic      int
	pkgs    []*Pkg
	instOut string // "back" or "quit": where esc goes
}

// ── messages ───────────────────────────────────────────────────────────────
type refreshMsg struct {
	rows  []toolRow
	dirty int
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
	m := &model{repo: r, busy: "scanning…"}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// runTUIInstall opens the TUI straight on the install picker (dot install --pick).
func runTUIInstall(r *Repo, o InstallOpts) error {
	m := &model{repo: r, busy: "scanning…", instOut: "quit"}
	m.openInstall(o)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) Init() tea.Cmd {
	if m.scr == scrInstall {
		return tea.Batch(m.refresh(), m.loadPkgs())
	}
	return m.refresh()
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
		return refreshMsg{rows: rows, dirty: gitDirty(r.Root)}
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
		m.rows, m.dirty, m.busy = msg.rows, msg.dirty, ""
		if t := m.curTool(); t != nil && m.fc >= len(t.entries) {
			m.fc = max(0, len(t.entries)-1)
		}
		return m, nil
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
		}
	}
	return m, nil
}

func (m *model) start(label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.busy, m.msg = label, ""
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
	head := stTitle.Render("dot") + stDim.Render("  "+m.repo.Tilde(m.repo.Root))
	if m.dirty > 0 {
		head += stUp.Render(fmt.Sprintf("  ● %d uncommitted change(s)", m.dirty))
	}
	b.WriteString(head + "\n\n")

	var body, help string
	switch m.scr {
	case scrTools:
		body = m.viewTools()
		help = keys("↑↓", "move", "enter", "files", "s", "sync tool", "S", "sync all", "i", "install", "f", "fonts", "c", "commit", "r", "rescan", "q", "quit")
	case scrFiles:
		body = m.viewFiles()
		help = keys("enter", "diff", "s", "sync", "p", "repo→machine", "i", "machine→repo", "m", "merge", "a", "sync all", "esc", "back")
	case scrDiff:
		b.Reset()
		b.WriteString(stTitle.Render(m.title) + stDim.Render("  --- repo  +++ machine") + "\n")
		b.WriteString(m.vp.View() + "\n")
		b.WriteString(keys("↑↓/pgup/pgdn", "scroll", "esc", "back"))
		return b.String()
	case scrFonts:
		body = m.viewFonts()
		help = keys("i", "install", "I", "install missing", "a", "add to fonts.toml", "r", "recheck", "esc", "back")
	case scrInstall:
		body = m.viewInstall()
		help = keys("space", "tick", "a", "all", "n", "none", "b", "packages", "f", "fonts", "enter", "install", "esc", "back")
	}
	b.WriteString(body)
	b.WriteString("\n")
	switch {
	case m.busy != "":
		b.WriteString(stDim.Render(m.busy))
	case m.msgErr:
		b.WriteString(stBad.Render("✗ " + m.msg))
	case m.msg != "":
		b.WriteString(stOK.Render("✓ " + m.msg))
	}
	b.WriteString("\n" + help)
	return b.String()
}

func keys(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, stKey.Render(pairs[i])+" "+stDim.Render(pairs[i+1]))
	}
	return strings.Join(parts, stDim.Render("  ·  "))
}

func cursor(on bool) string {
	if on {
		return stCursor.Render("▸ ")
	}
	return "  "
}

func (m *model) viewTools() string {
	if len(m.rows) == 0 {
		return stDim.Render("  no tools in tools.toml") + "\n"
	}
	var b strings.Builder
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
			sum = append(sum, stDim.Render("nothing found on machine or in repo"))
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
		name := lipgloss.NewStyle().Width(10).Bold(i == m.tc).Render(t.tool.Name)
		desc := lipgloss.NewStyle().Width(28).Render(stDim.Render(t.tool.Description))
		fmt.Fprintf(&b, "%s%s %s %s\n", cursor(i == m.tc), name, desc, strings.Join(sum, "  "))
	}
	b.WriteString("\n" + stDim.Render("  ↓ apply = repo → this machine   ↑ import = this machine → repo   ⇅ merge both") + "\n")
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
	avail := max(3, m.h-9)
	lo := 0
	if m.fc >= avail {
		lo = m.fc - avail + 1
	}
	hi := min(len(t.entries), lo+avail)
	for i := lo; i < hi; i++ {
		e := t.entries[i]
		st := statusStyle(e.Status).Width(16).Render(e.Status.Symbol() + " " + e.Status.String())
		fmt.Fprintf(&b, "%s%s ~/%s\n", cursor(i == m.fc), st, e.Rel)
	}
	if hi < len(t.entries) || lo > 0 {
		b.WriteString(stDim.Render(fmt.Sprintf("  … %d/%d", m.fc+1, len(t.entries))) + "\n")
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
