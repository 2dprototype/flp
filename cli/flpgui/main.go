// Command flp-gui is a desktop toolbox for FL Studio (.flp) project files.
//
// Features: semantic diff, project inspector, visualiser (with scroll),
// channel / pattern / mixer browsers, git integration, and an editor.
//
// Usage:
//
//	flp-gui                 open with no project loaded
//	flp-gui path/to/x.flp   open with that project preloaded
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/2dprototype/flp"
	"github.com/gonutz/wui/v2"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

// ───────────────────────── constants & state ─────────────────────────

const (
	defaultW = 700
	defaultH = 540
	version  = "v0.0.1"

	vizHeaderH    = 44
	scrollbarSize = 12
	rulerH        = 24
	trackHdrW     = 160
	pianoKeysW    = 60

	basePxPerTick = 0.15
	baseTrackH    = 24
	baseKeyH      = 12

	margin      = 20
	rowGap      = 10
	editH       = 28
	labelH      = 22
	btnH        = 30
	closeBtnW   = 100
	fileLabelW  = 80
	fileBtnW    = 120
	rowInnerGap = 8
	sidebarW    = 220
)

// Clean Light Palette
var (
	colBG       = wui.RGB(248, 249, 251)
	colSidebar  = wui.RGB(238, 240, 244)
	colHeader   = wui.RGB(226, 229, 235)
	colHeaderFG = wui.RGB(28, 32, 40)
	colHeaderDi = wui.RGB(108, 116, 128)
	colCanvasBG = wui.RGB(252, 253, 255)
	colTrackBG  = wui.RGB(255, 255, 255)
	colTrackAlt = wui.RGB(246, 248, 251)
	colClipBG   = wui.RGB(90, 140, 210)
	colClipEdge = wui.RGB(60, 100, 160)
	colText     = wui.RGB(40, 44, 52)
	colTextDim  = wui.RGB(120, 128, 140)
	colTrack    = wui.RGB(232, 235, 240)
	colThumb    = wui.RGB(160, 168, 180)
	colThumbHi  = wui.RGB(130, 138, 150)
	colGrid     = wui.RGB(216, 220, 226)
	colGridBeat = wui.RGB(233, 236, 241)
	colSep      = wui.RGB(210, 215, 222)
)

var (
	fontNormal *wui.Font
	fontBold   *wui.Font
	fontTitle  *wui.Font
	fontLarge  *wui.Font
)

func makeFont(name string, px int, bold bool) *wui.Font {
	f, err := wui.NewFont(wui.FontDesc{Name: name, Height: -px, Bold: bold})
	if err != nil {
		return nil
	}
	return f
}

func init() {
	fontNormal = makeFont("Segoe UI", 13, false)
	fontBold = makeFont("Segoe UI", 13, true)
	fontTitle = makeFont("Segoe UI", 16, true)
	fontLarge = makeFont("Segoe UI", 24, true)
}

// AppState manages the globally loaded project to avoid redundant file prompts.
type AppState struct {
	Project  *flp.FLPProject
	Original *flp.FLPProject
	Path     string
	SaveAs   string
	OnUpdate func()
}

var app AppState

// ───────────────────────── text & ui helpers ─────────────────────────

func setText(t *wui.TextEdit, text string) {
	t.SetText(strings.ReplaceAll(text, "\n", "\r\n"))
}

func parseFloat(s string) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return v
	}
	return 0
}

func parseInt(s string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return 0
}

func fmtFloatShort(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func newLabel(text string, x, y, w, h int) *wui.Label {
	l := wui.NewLabel()
	l.SetText(text)
	l.SetBounds(x, y, w, h)
	if fontNormal != nil {
		l.SetFont(fontNormal)
	}
	return l
}

func newBtn(text string, x, y, w, h int, onClick func()) *wui.Button {
	b := wui.NewButton()
	b.SetText(text)
	b.SetBounds(x, y, w, h)
	if onClick != nil {
		b.SetOnClick(onClick)
	}
	if fontNormal != nil {
		b.SetFont(fontNormal)
	}
	return b
}

func newEdit(x, y, w, h int) *wui.EditLine {
	e := wui.NewEditLine()
	e.SetBounds(x, y, w, h)
	if fontNormal != nil {
		e.SetFont(fontNormal)
	}
	return e
}

func newCombo(items []string, x, y, w, h int) *wui.ComboBox {
	c := wui.NewComboBox()
	c.SetItems(items)
	c.SetSelectedIndex(0)
	c.SetBounds(x, y, w, h)
	if fontNormal != nil {
		c.SetFont(fontNormal)
	}
	return c
}

func newOutput(x, y, w, h int) *wui.TextEdit {
	t := wui.NewTextEdit()
	t.SetBounds(x, y, w, h)
	t.SetReadOnly(true)
	t.SetWordWrap(false)
	if fontNormal != nil {
		t.SetFont(fontNormal)
	}
	return t
}

func applyLayout(w *wui.Window, fn func(iw, ih int)) {
	doLayout := func() {
		iw, ih := w.InnerWidth(), w.InnerHeight()
		if iw <= 0 {
			iw = defaultW
		}
		if ih <= 0 {
			ih = defaultH
		}
		fn(iw, ih)
	}
	w.SetOnResize(doLayout)
	w.SetOnShow(doLayout)
	doLayout()
}

func browseFLP(parent *wui.Window, title string) string {
	dlg := wui.NewFileOpenDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp, *.fst)", "flp", "fst")
	dlg.AddFilter("All files", "*.*")
	if ok, path := dlg.ExecuteSingleSelection(parent); ok {
		return path
	}
	return ""
}

func browseSave(parent *wui.Window, title string) string {
	dlg := wui.NewFileSaveDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp)", "flp")
	if ok, path := dlg.Execute(parent); ok {
		return path
	}
	return ""
}

func browseSaveMIDI(parent *wui.Window, title string) string {
	dlg := wui.NewFileSaveDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("MIDI file (*.mid)", "mid")
	if ok, path := dlg.Execute(parent); ok {
		return path
	}
	return ""
}

func exportPatternMIDI(p *flp.FLPProject, patternIdx int, path string) error {
	if p == nil {
		return fmt.Errorf("no project loaded")
	}
	if patternIdx < 0 || patternIdx >= len(p.Patterns) {
		return fmt.Errorf("pattern index out of range")
	}
	pat := p.Patterns[patternIdx]

	ppq := uint16(96)
	if p.Header.PPQ > 0 {
		ppq = uint16(p.Header.PPQ)
	}
	clock := smf.MetricTicks(ppq)

	channelMap := make(map[int]uint8)
	nextCh := uint8(0)
	for _, n := range pat.Notes {
		if _, ok := channelMap[n.ChannelIid]; !ok {
			channelMap[n.ChannelIid] = nextCh
			nextCh = (nextCh + 1) % 16
		}
	}

	type midiEvent struct {
		tick int64
		msg  midi.Message
	}
	events := make([]midiEvent, 0, len(pat.Notes)*2)

	for _, n := range pat.Notes {
		ch := channelMap[n.ChannelIid]
		vel := uint8(n.Velocity)
		if vel == 0 {
			vel = 100
		}
		events = append(events, midiEvent{tick: int64(n.Position), msg: midi.NoteOn(ch, uint8(n.Key), vel)})
		events = append(events, midiEvent{tick: int64(n.Position + n.Length), msg: midi.NoteOff(ch, uint8(n.Key))})
	}

	sort.Slice(events, func(i, j int) bool { return events[i].tick < events[j].tick })

	var tr smf.Track
	var lastTick int64
	for _, ev := range events {
		delta := ev.tick - lastTick
		if delta < 0 {
			delta = 0
		}
		tr.Add(uint32(delta), ev.msg)
		lastTick = ev.tick
	}
	tr.Close(0)

	s := smf.New()
	s.TimeFormat = clock
	s.Add(tr)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = s.WriteTo(f)
	return err
}

func loadProject(path string) (*flp.FLPProject, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return flp.ParseFLPFile(buf)
}

func formatErr(err error) string {
	if pe, ok := err.(*flp.FLPParseError); ok {
		return "Parse error:\n" + pe.Error()
	}
	return "Error: " + err.Error()
}

func marshalIndentedJSON(v interface{}) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

const maxOutBytes = 2 * 1024 * 1024

func trimForUI(s string) string {
	if len(s) <= maxOutBytes {
		return s
	}
	return s[:maxOutBytes] + "\n\n[... output truncated ...]"
}

// ───────────────────────── main window ─────────────────────────

func main() {
	// CLI: flp-gui <path.flp>  → preload that project.
	if len(os.Args) > 1 {
		path := os.Args[1]
		if path == "-h" || path == "--help" {
			fmt.Fprintf(os.Stderr, "Usage: %s [path/to/project.flp]\n", filepath.Base(os.Args[0]))
			os.Exit(0)
		}
		p, err := loadProject(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flp-gui: failed to load %s: %v\n", path, err)
			// Fall through and show the window anyway so the user can pick a
			// different file via the GUI.
		} else {
			app.Project = p
			app.Original = p
			app.Path = path
		}
	}
	newMainWindow().Show()
}

func newMainWindow() *wui.Window {
	w := wui.NewWindow()
	w.SetTitle("FLP (FL Studio Project) Tool")
	w.SetSize(defaultW, defaultH)
	w.SetResizable(true)
	w.SetBackground(colBG)
	w.SetHasMinButton(true)
	w.SetHasMaxButton(true)

	// Sidebar paint box
	sbPaint := wui.NewPaintBox()
	w.Add(sbPaint)

	title := wui.NewLabel()
	title.SetText("FLP TOOL")
	if fontTitle != nil {
		title.SetFont(fontTitle)
	}
	w.Add(title)

	verLabel := newLabel(version, 0, 0, 100, labelH)
	w.Add(verLabel)

	// Dashboard components
	dashPaint := wui.NewPaintBox()
	w.Add(dashPaint)

	btnLoad := newBtn("Load Project...", 0, 0, 140, 40, nil)
	w.Add(btnLoad)

	tools := []struct {
		label string
		open  func(*wui.Window)
	}{
		{"Visualizer", openVisualizerTool},
		{"Edit Project", openEditTool},
		{"Diff Two Projects", openDiffTool},
		{"Inspect JSON/Canon", openInspectTool},
		{"Channel Browser", openChannelTool},
		{"Pattern Browser", openPatternTool},
		{"Mixer Browser", openMixerTool},
		{"Git Integration", openGitTool},
		{"About", openAboutTool},
	}

	btns := make([]*wui.Button, len(tools))
	for i, t := range tools {
		openFn := t.open
		b := newBtn(t.label, 0, 0, 100, 36, func() { openFn(w) })
		w.Add(b)
		btns[i] = b
	}

	app.OnUpdate = func() {
		dashPaint.Paint()
		for _, b := range btns {
			// Require project for most tools, except About, Diff, Git.
			if app.Project == nil && b.Text() != "Diff Two Projects" && b.Text() != "About" && b.Text() != "Git Integration" {
				b.SetEnabled(false)
			} else {
				b.SetEnabled(true)
			}
		}
	}

	btnLoad.SetOnClick(func() {
		if path := browseFLP(w, "Select FL Studio project"); path != "" {
			p, err := loadProject(path)
			if err != nil {
				wui.MessageBoxError("Load Error", err.Error())
				return
			}
			app.Project = p
			app.Original = p
			app.Path = path
			app.SaveAs = ""
			app.OnUpdate()
		}
	})

	sbPaint.SetOnPaint(func(c *wui.Canvas) {
		iw, ih := c.Size()
		c.FillRect(0, 0, iw, ih, colSidebar)
		c.Line(iw-1, 0, iw-1, ih, colSep)
	})

	dashPaint.SetOnPaint(func(c *wui.Canvas) {
		iw, ih := c.Size()
		c.FillRect(0, 0, iw, ih, colBG)

		if app.Project == nil {
			c.TextOut(40, ih/2-20, "No Project Loaded.", colTextDim)
			return
		}

		c.TextOut(40, 40, filepath.Base(app.Path), colHeaderFG)

		tempo := 120.0
		if t := flp.GetTempo(app.Project); t != nil {
			tempo = *t
		}

		stats := []string{
			fmt.Sprintf("Tempo: %.2f BPM", tempo),
			fmt.Sprintf("PPQ: %d", app.Project.Header.PPQ),
			fmt.Sprintf("Channels: %d", len(app.Project.Channels)),
			fmt.Sprintf("Patterns: %d", len(app.Project.Patterns)),
			fmt.Sprintf("Mixer Inserts: %d", len(app.Project.Inserts)),
			fmt.Sprintf("Arrangements: %d", len(app.Project.Arrangements)),
		}

		y := 90
		for _, stat := range stats {
			c.TextOut(40, y, stat, colText)
			y += 30
		}
	})

	applyLayout(w, func(iw, ih int) {
		sbPaint.SetBounds(0, 0, sidebarW, ih)
		title.SetBounds(margin, 20, sidebarW-2*margin, 30)
		verLabel.SetBounds(margin, ih-30, sidebarW-2*margin, labelH)

		dashPaint.SetBounds(sidebarW, 0, iw-sidebarW, ih)

		btnLoad.SetBounds(sidebarW+40, ih-80, 160, 40)

		y := 80
		for _, b := range btns {
			b.SetBounds(margin, y, sidebarW-2*margin, 36)
			y += 42
		}
	})

	app.OnUpdate()
	return w
}

func newModal(title string) *wui.Window {
	w := wui.NewWindow()
	w.SetTitle(title)
	w.SetSize(defaultW, defaultH)
	w.SetResizable(true)
	w.SetBackground(colBG)
	w.SetHasMinButton(true)
	w.SetHasMaxButton(true)
	return w
}

func showModal(w *wui.Window) {
	if err := w.ShowModal(); err != nil {
		fmt.Fprintln(os.Stderr, "flp-gui:", err)
	}
}

// ───────────────────────── 1. Diff ─────────────────────────

func openDiffTool(_ *wui.Window) {
	w := newModal("Diff Two Projects")

	lblA := newLabel("File A (Current):", 0, 0, 120, labelH)
	w.Add(lblA)
	pathA := "No project loaded"
	if app.Path != "" {
		pathA = app.Path
	}
	edA := newEdit(0, 0, 100, editH)
	edA.SetText(pathA)
	edA.SetReadOnly(true)
	w.Add(edA)

	lblB := newLabel("File B:", 0, 0, 120, labelH)
	w.Add(lblB)
	edB := newEdit(0, 0, 100, editH)
	w.Add(edB)
	btnBrowse := newBtn("Browse...", 0, 0, 90, editH, nil)
	w.Add(btnBrowse)

	btnDiff := newBtn("Diff", 0, 0, 100, btnH, nil)
	w.Add(btnDiff)
	btnVerbose := newBtn("Verbose", 0, 0, 100, btnH, nil)
	w.Add(btnVerbose)

	out := newOutput(0, 0, 100, 100)
	setText(out, "Select File B to compare against the currently loaded project.")
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		lblA.SetBounds(margin, y+3, 120, labelH)
		edA.SetBounds(margin+120, y, iw-120-2*margin, editH)
		y += editH + rowGap

		lblB.SetBounds(margin, y+3, 120, labelH)
		edW := iw - 120 - 90 - 2*margin - rowInnerGap
		if edW < 100 {
			edW = 100
		}
		edB.SetBounds(margin+120, y, edW, editH)
		btnBrowse.SetBounds(margin+120+edW+rowInnerGap, y, 90, editH)
		y += editH + rowGap + 6

		btnDiff.SetBounds(margin, y, 100, btnH)
		btnVerbose.SetBounds(margin+108, y, 100, btnH)
		y += btnH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, iw-2*margin, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	btnBrowse.SetOnClick(func() {
		if p := browseFLP(w, "Select Project B"); p != "" {
			edB.SetText(p)
		}
	})

	run := func(verbose bool) {
		if app.Project == nil {
			setText(out, "Error: File A (Main Project) is not loaded.")
			return
		}
		b := edB.Text()
		if b == "" {
			setText(out, "Error: File B must be selected.")
			return
		}
		pb, err := loadProject(b)
		if err != nil {
			setText(out, formatErr(err))
			return
		}
		res := flp.CompareProjects(app.Project, pb)
		t := fmt.Sprintf("%s vs %s", filepath.Base(app.Path), filepath.Base(b))
		body := flp.RenderSummary(res, flp.RenderSummaryOptions{Title: t, Verbose: verbose})
		if !flp.DiffSummaryHasChanges(res.Summary) {
			body += "\n\nNo differences. The two projects are identical."
		}
		setText(out, trimForUI(body))
	}
	btnDiff.SetOnClick(func() { run(false) })
	btnVerbose.SetOnClick(func() { run(true) })

	showModal(w)
}

// ───────────────────────── 2. Edit & Save ─────────────────────────

type propField struct {
	label *wui.Label
	edit  *wui.EditLine
}

type categoryDef struct {
	label string
	list  func(*flp.FLPProject) []string
	read  func(*flp.FLPProject, int) []string
	props []string
	apply func(*flp.FLPProject, int, []string) (*flp.FLPProject, error)
	msg   func(int, []string) string
}

func colorStrings(c *flp.RGBA) []string {
	if c == nil {
		return []string{"", "", "", ""}
	}
	return []string{strconv.Itoa(c.R), strconv.Itoa(c.G), strconv.Itoa(c.B), strconv.Itoa(c.A)}
}

func parseColorStrings(vals []string) flp.MutRGBA {
	get := func(i int) float64 {
		if i < 0 || i >= len(vals) {
			return 0
		}
		return parseFloat(vals[i])
	}
	return flp.MutRGBA{R: get(0), G: get(1), B: get(2), A: get(3)}
}

// clampF restricts v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// allEmpty reports whether every string is whitespace-only.
func allEmpty(vals []string) bool {
	for _, s := range vals {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// validIntString returns the decimal representation of v if v is within
// [lo, hi], or "" if it's outside the range the corresponding setter accepts.
func validIntString(v, lo, hi int) string {
	if v < lo || v > hi {
		return ""
	}
	return strconv.Itoa(v)
}

func arrTrackAt(p *flp.FLPProject, i int) (int, int, bool) {
	n := 0
	for ai, a := range p.Arrangements {
		for ti := range a.Tracks {
			if n == i {
				return ai, ti, true
			}
			n++
		}
	}
	return 0, 0, false
}

var editCategories = []categoryDef{
	{
		label: "Tempo",
		list:  func(*flp.FLPProject) []string { return []string{"(project tempo)"} },
		read: func(p *flp.FLPProject, _ int) []string {
			t := flp.GetTempo(p)
			if t == nil {
				return []string{"120"}
			}
			return []string{fmtFloatShort(*t)}
		},
		props: []string{"BPM"},
		apply: func(p *flp.FLPProject, _ int, v []string) (*flp.FLPProject, error) {
			return flp.SetTempo(p, parseFloat(v[0]))
		},
		msg: func(_ int, v []string) string { return "Tempo → " + v[0] + " BPM" },
	},
	{
		label: "Time signature",
		list:  func(*flp.FLPProject) []string { return []string{"(project signature)"} },
		read: func(p *flp.FLPProject, _ int) []string {
			num, den := "4", "4"
			if p.Metadata.TimeSignatureNumerator != nil {
				num = strconv.Itoa(*p.Metadata.TimeSignatureNumerator)
			}
			if p.Metadata.TimeSignatureDenominator != nil {
				den = strconv.Itoa(*p.Metadata.TimeSignatureDenominator)
			}
			return []string{num, den}
		},
		props: []string{"Numerator", "Denominator"},
		apply: func(p *flp.FLPProject, _ int, v []string) (*flp.FLPProject, error) {
			return flp.SetTimeSignature(p, parseInt(v[0]), parseInt(v[1]))
		},
		msg: func(_ int, v []string) string {
			return "Time signature → " + v[0] + "/" + v[1]
		},
	},
	{
		label: "Channels",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Channels))
			for i, c := range p.Channels {
				n := fmt.Sprintf("#%d", c.Iid)
				if c.Name != nil && *c.Name != "" {
					n = *c.Name
				}
				out[i] = fmt.Sprintf("%d   %s   [%s]", c.Iid, n, c.Kind)
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Channels) {
				return make([]string, 8)
			}
			c := p.Channels[i]

			name := ""
			if c.Name != nil {
				name = *c.Name
			}

			// Volume/Pan: leave blank if the raw FL value is outside the
			// range the setters accept, so we don't push garbage into them.
			vol, pan := "", ""
			if c.Levels != nil {
				rawVol := float64(c.Levels.Volume)
				rawPan := float64(c.Levels.Pan)
				if rawVol >= 0 && rawVol <= 12800 {
					vol = fmtFloatShort(rawVol / 12800.0)
				}
				if rawPan >= -6400 && rawPan <= 6400 {
					pan = fmtFloatShort(rawPan / 6400.0)
				}
			}

			// Routing: only emit if it's in the setter's accepted range.
			ti := ""
			if c.TargetInsert != nil {
				ti = validIntString(*c.TargetInsert, -1, 127)
			}

			col := colorStrings(c.Color)
			return []string{name, vol, pan, ti, col[0], col[1], col[2], col[3]}
		},
		props: []string{
			"Name", "Volume (0..1)", "Pan (-1..+1)", "Target insert",
			"Color.R", "Color.G", "Color.B", "Color.A",
		},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Channels) {
				return nil, fmt.Errorf("channel index out of range")
			}
			iid := p.Channels[i].Iid
			cur := p
			var err error

			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetChannelName(cur, iid, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				if cur, err = flp.SetChannelVolume(cur, iid, parseFloat(v[1])); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[2]) != "" {
				if cur, err = flp.SetChannelPan(cur, iid, parseFloat(v[2])); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[3]) != "" {
				if cur, err = flp.SetChannelRouting(cur, iid, parseInt(v[3])); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[4:]) {
				if cur, err = flp.SetChannelColor(cur, iid, parseColorStrings(v[4:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Channel index %d updated", i)
		},
	},
	{
		label: "Patterns",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Patterns))
			for i, pt := range p.Patterns {
				n := fmt.Sprintf("#%d", pt.ID)
				if pt.Name != nil && *pt.Name != "" {
					n = *pt.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d notes)", pt.ID, n, len(pt.Notes))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Patterns) {
				return make([]string, 6)
			}
			pt := p.Patterns[i]
			name := ""
			if pt.Name != nil {
				name = *pt.Name
			}
			length := ""
			if pt.Length != nil {
				length = strconv.Itoa(int(*pt.Length))
			}
			col := colorStrings(pt.Color)
			return []string{name, length, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Length (ticks)", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Patterns) {
				return nil, fmt.Errorf("pattern index out of range")
			}
			pid := p.Patterns[i].ID
			cur := p
			var err error
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetPatternName(cur, pid, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				if cur, err = flp.SetPatternLength(cur, pid, int64(parseInt(v[1]))); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[2:]) {
				if cur, err = flp.SetPatternColor(cur, pid, parseColorStrings(v[2:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Pattern index %d updated", i)
		},
	},
	{
		label: "Mixer inserts",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Inserts))
			for i, ins := range p.Inserts {
				n := "(unnamed)"
				if ins.Name != nil && *ins.Name != "" {
					n = *ins.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d slots)", ins.Index, n, len(ins.Slots))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Inserts) {
				return make([]string, 5)
			}
			ins := p.Inserts[i]
			name := ""
			if ins.Name != nil {
				name = *ins.Name
			}
			col := colorStrings(ins.Color)
			return []string{name, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Inserts) {
				return nil, fmt.Errorf("insert index out of range")
			}
			idx := p.Inserts[i].Index
			cur := p
			var err error

			// Only write the name if the user actually typed something, so an
			// empty field doesn't insert a spurious empty-name event.
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetInsertName(cur, idx, v[0]); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[1:]) {
				if cur, err = flp.SetInsertColor(cur, idx, parseColorStrings(v[1:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Insert index %d updated", i)
		},
	},
	{
		label: "Arrangements",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Arrangements))
			for i, a := range p.Arrangements {
				n := fmt.Sprintf("#%d", a.ID)
				if a.Name != nil && *a.Name != "" {
					n = *a.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d tracks)", a.ID, n, len(a.Tracks))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Arrangements) {
				return []string{""}
			}
			if n := p.Arrangements[i].Name; n != nil {
				return []string{*n}
			}
			return []string{""}
		},
		props: []string{"Name"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Arrangements) {
				return nil, fmt.Errorf("arrangement index out of range")
			}
			if strings.TrimSpace(v[0]) == "" {
				return nil, fmt.Errorf("name cannot be empty")
			}
			return flp.SetArrangementName(p, p.Arrangements[i].ID, v[0])
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Arrangement index %d renamed", i)
		},
	},
	{
		label: "Tracks",
		list: func(p *flp.FLPProject) []string {
			out := []string{}
			for _, a := range p.Arrangements {
				for ti, t := range a.Tracks {
					name := ""
					if t.Name != nil {
						name = *t.Name
					}
					label := fmt.Sprintf("Arr %d / T%-2d", a.ID, ti)
					if name != "" {
						label += "   " + name
					}
					out = append(out, label)
				}
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			ai, ti, ok := arrTrackAt(p, i)
			if !ok {
				return make([]string, 6)
			}
			t := p.Arrangements[ai].Tracks[ti]
			name := ""
			if t.Name != nil {
				name = *t.Name
			}
			grouped := ""
			if t.Grouped != nil {
				if *t.Grouped {
					grouped = "1"
				} else {
					grouped = "0"
				}
			}
			col := colorStrings(t.Color)
			return []string{name, grouped, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Grouped (0/1)", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			ai, ti, ok := arrTrackAt(p, i)
			if !ok {
				return nil, fmt.Errorf("track index out of range")
			}
			arrID := p.Arrangements[ai].ID
			cur := p
			var err error
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetTrackName(cur, arrID, ti, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				grouped := parseInt(v[1]) != 0
				if cur, err = flp.SetTrackGrouped(cur, arrID, ti, grouped); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[2:]) {
				if cur, err = flp.SetTrackColor(cur, arrID, ti, parseColorStrings(v[2:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Track index %d updated", i)
		},
	},
}

func openEditTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Edit Project", "Please load a project first.")
		return
	}

	w := newModal("Edit Project - " + filepath.Base(app.Path))

	lblCatH := newLabel("Category", 0, 0, 100, 16)
	w.Add(lblCatH)
	lblItemH := newLabel("Item", 0, 0, 100, 16)
	w.Add(lblItemH)
	lblPropH := newLabel("Properties", 0, 0, 100, 16)
	w.Add(lblPropH)

	catList := wui.NewStringList()
	if fontNormal != nil {
		catList.SetFont(fontNormal)
	}
	w.Add(catList)

	itemList := wui.NewStringList()
	if fontNormal != nil {
		itemList.SetFont(fontNormal)
	}
	w.Add(itemList)

	const maxFields = 8
	fields := make([]propField, maxFields)
	for i := range fields {
		lbl := newLabel("", 0, 0, 100, labelH)
		ed := newEdit(0, 0, 100, editH)
		w.Add(lbl)
		w.Add(ed)
		fields[i] = propField{lbl, ed}
	}

	status := newLabel("Editing active project memory.", 0, 0, 100, labelH)
	w.Add(status)
	btnRevert := newBtn("Revert to Disk", 0, 0, 130, btnH, nil)
	w.Add(btnRevert)
	btnApply := newBtn("Apply Change", 0, 0, 130, btnH, nil)
	w.Add(btnApply)
	btnSave := newBtn("Save FLP...", 0, 0, 110, btnH, nil)
	w.Add(btnSave)
	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		headerY := y
		y += 18

		bottomH := btnH + 2*margin
		contentBottom := ih - bottomH
		contentH := contentBottom - y
		if contentH < 80 {
			contentH = 80
		}

		const gap = 6
		availW := contentW - 2*gap
		catW := availW * 24 / 100
		itemW := availW * 30 / 100
		propW := availW - catW - itemW

		catX := margin
		itemX := catX + catW + gap
		propX := itemX + itemW + gap

		lblCatH.SetBounds(catX, headerY, catW, 16)
		lblItemH.SetBounds(itemX, headerY, itemW, 16)
		lblPropH.SetBounds(propX, headerY, propW, 16)

		catList.SetBounds(catX, y, catW, contentH)
		itemList.SetBounds(itemX, y, itemW, contentH)

		fieldH := contentH / maxFields
		if fieldH < 26 {
			fieldH = 26
		}
		if fieldH > 40 {
			fieldH = 40
		}
		const propLabelW = 100
		for i, f := range fields {
			fy := y + i*fieldH
			f.label.SetBounds(propX, fy+4, propLabelW, fieldH-8)
			f.edit.SetBounds(propX+propLabelW+4, fy+2, propW-propLabelW-4, fieldH-4)
		}

		barY := ih - margin - btnH

		btnSave.SetBounds(margin, barY, 110, btnH)
		btnClose.SetBounds(iw-margin-closeBtnW, barY, closeBtnW, btnH)
		btnApply.SetBounds(iw-margin-closeBtnW-rowInnerGap-130, barY, 130, btnH)
		btnRevert.SetBounds(iw-margin-closeBtnW-rowInnerGap-130-rowInnerGap-130, barY, 130, btnH)

		statusX := margin + 110 + rowInnerGap
		statusW := iw - statusX - (2*130 + closeBtnW + 3*rowInnerGap + margin)
		if statusW < 60 {
			statusW = 60
		}
		status.SetBounds(statusX, barY+4, statusW, labelH)
	})

	catIndex := 0
	itemIndex := 0

	clearFields := func() {
		for i := range fields {
			fields[i].label.SetText("")
			fields[i].edit.SetText("")
		}
	}

	populateFields := func(cat int, idx int) {
		clearFields()
		if app.Project == nil || cat < 0 || cat >= len(editCategories) {
			return
		}
		vals := editCategories[cat].read(app.Project, idx)
		for i, p := range editCategories[cat].props {
			if i >= len(fields) {
				break
			}
			fields[i].label.SetText(p)
			if i < len(vals) {
				fields[i].edit.SetText(vals[i])
			}
		}
	}

	readFields := func(cat int) []string {
		if cat < 0 || cat >= len(editCategories) {
			return nil
		}
		out := make([]string, len(editCategories[cat].props))
		for i := range out {
			if i < len(fields) {
				out[i] = fields[i].edit.Text()
			}
		}
		return out
	}

	refreshItems := func() {
		if app.Project == nil || catIndex < 0 || catIndex >= len(editCategories) {
			itemList.SetItems([]string{})
			clearFields()
			return
		}
		cat := editCategories[catIndex]
		items := cat.list(app.Project)
		itemList.SetItems(items)
		keep := itemIndex
		if keep < 0 || keep >= len(items) {
			keep = 0
		}
		if len(items) > 0 {
			itemList.SetSelectedIndex(keep)
			itemIndex = keep
			populateFields(catIndex, keep)
		} else {
			itemIndex = -1
			clearFields()
		}
	}

	catLabels := make([]string, len(editCategories))
	for i, c := range editCategories {
		catLabels[i] = c.label
	}
	catList.SetItems(catLabels)
	catList.SetSelectedIndex(0)
	catList.SetOnChange(func(i int) {
		if i < 0 {
			return
		}
		catIndex = i
		itemIndex = 0
		refreshItems()
	})

	itemList.SetOnChange(func(i int) {
		if i < 0 || catIndex < 0 || catIndex >= len(editCategories) {
			return
		}
		itemIndex = i
		populateFields(catIndex, i)
	})

	btnApply.SetOnClick(func() {
		if app.Project == nil || catIndex < 0 || catIndex >= len(editCategories) || itemIndex < 0 {
			return
		}
		cat := editCategories[catIndex]
		vals := readFields(catIndex)
		next, err := cat.apply(app.Project, itemIndex, vals)
		if err != nil {
			status.SetText("Apply failed: " + err.Error())
			fmt.Fprintf(os.Stderr, "[edit] apply failed (%s, item=%d, vals=%q): %v\n",
				cat.label, itemIndex, vals, err)
			return
		}
		app.Project = next
		app.OnUpdate()
		status.SetText(cat.msg(itemIndex, vals))
		refreshItems()
	})

	btnRevert.SetOnClick(func() {
		if app.Original == nil {
			return
		}
		app.Project = app.Original
		app.OnUpdate()
		status.SetText("Reverted memory to last saved disk state.")
		refreshItems()
	})

	btnSave.SetOnClick(func() {
		path := browseSave(w, "Save Project")
		if path == "" {
			return
		}
		data, err := flp.SerializeFLPProject(app.Project)
		if err != nil {
			status.SetText("Serialize error: " + err.Error())
			return
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			status.SetText("Write error: " + err.Error())
			return
		}
		app.Original = app.Project
		app.Path = path
		app.OnUpdate()
		status.SetText(fmt.Sprintf("Saved to %s", filepath.Base(path)))
	})

	refreshItems()
	showModal(w)
}

// ───────────────────────── 3. Inspect ─────────────────────────

func openInspectTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Inspect Project", "Please load a project first.")
		return
	}

	w := newModal("Inspect Project")

	lblFmt := newLabel("Format:", 0, 0, 60, labelH)
	w.Add(lblFmt)
	cmb := newCombo([]string{"text", "canonical", "json"}, 0, 0, 140, editH)
	w.Add(cmb)

	btnShow := newBtn("Refresh", 0, 0, 100, btnH, nil)
	w.Add(btnShow)

	out := newOutput(0, 0, 100, 100)
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		lblFmt.SetBounds(margin, y+3, 60, labelH)
		cmb.SetBounds(margin+60, y, 140, editH)
		btnShow.SetBounds(margin+60+140+rowGap, y-1, 100, btnH)
		y += editH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, iw-2*margin, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	updateOutput := func() {
		if app.Project == nil {
			setText(out, "No project loaded.")
			return
		}
		switch cmb.Items()[cmb.SelectedIndex()] {
		case "canonical":
			setText(out, trimForUI(flp.RenderCanonical(app.Project)))
		case "json":
			s, jerr := marshalIndentedJSON(flp.ToFlpInfoJson(app.Project))
			if jerr != nil {
				setText(out, formatErr(jerr))
				return
			}
			setText(out, trimForUI(s))
		default:
			setText(out, flp.RenderInfo(app.Project, app.Path))
		}
	}
	btnShow.SetOnClick(updateOutput)
	cmb.SetOnChange(func(i int) { updateOutput() })

	updateOutput()
	showModal(w)
}

// ───────────────────────── 4. Visualizer ─────────────────────────

type vizState struct {
	project *flp.FLPProject
	source  string
	view    string

	scrollX, scrollY   int
	contentW, contentH int
	vpW, vpH           int
	pbW, pbH           int

	zoomX, zoomY float64

	dragMode         int
	dragStartX       int
	dragStartY       int
	dragStartScrollX int
	dragStartScrollY int

	patternIdx int
}

func openVisualizerTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Visualizer", "Please load a project first.")
		return
	}

	w := newModal("Visualizer")
	w.SetBackground(colCanvasBG)

	lblView := newLabel("View:", 0, 0, 40, labelH)
	w.Add(lblView)
	cmb := newCombo([]string{"arrangement", "pianoroll", "channels", "patterns", "mixer"}, 0, 0, 110, editH)
	w.Add(cmb)

	lblPat := newLabel("Pattern:", 0, 0, 56, labelH)
	w.Add(lblPat)
	patCmb := newCombo([]string{}, 0, 0, 160, editH)
	w.Add(patCmb)

	lblZoom := newLabel("Zoom:", 0, 0, 44, labelH)
	w.Add(lblZoom)
	btnZoomOut := newBtn("−", 0, 0, 28, editH, nil)
	w.Add(btnZoomOut)
	btnZoomIn := newBtn("+", 0, 0, 28, editH, nil)
	w.Add(btnZoomIn)
	btnFit := newBtn("Fit", 0, 0, 40, editH, nil)
	w.Add(btnFit)

	btnExportMIDI := newBtn("Export MIDI", 0, 0, 100, editH, nil)
	w.Add(btnExportMIDI)

	pb := wui.NewPaintBox()
	w.Add(pb)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	s := &vizState{
		project:    app.Project,
		source:     app.Path,
		view:       "arrangement",
		zoomX:      1,
		zoomY:      1,
		patternIdx: 0,
	}

	patItems := make([]string, len(s.project.Patterns))
	for i, pt := range s.project.Patterns {
		n := fmt.Sprintf("#%d", pt.ID)
		if pt.Name != nil && *pt.Name != "" {
			n = *pt.Name
		}
		patItems[i] = fmt.Sprintf("%d: %s", i, n)
	}
	if len(patItems) == 0 {
		patItems = []string{"(no patterns)"}
	}
	patCmb.SetItems(patItems)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		x := margin
		lblView.SetBounds(x, y+3, 40, labelH)
		x += 40
		cmb.SetBounds(x, y, 110, editH)
		x += 120

		lblPat.SetBounds(x, y+3, 56, labelH)
		x += 56

		right := margin + contentW
		btnExportMIDI.SetBounds(right-100, y, 100, editH)
		right -= 100 + 8
		btnFit.SetBounds(right-40, y, 40, editH)
		right -= 40 + 6
		btnZoomIn.SetBounds(right-28, y, 28, editH)
		right -= 28 + 4
		btnZoomOut.SetBounds(right-28, y, 28, editH)
		right -= 28 + 8
		lblZoom.SetBounds(right-44, y+3, 44, labelH)
		right -= 44 + 10

		patW := right - x
		if patW < 100 {
			patW = 100
		}
		patCmb.SetBounds(x, y, patW, editH)

		y += editH + rowGap + 6

		bottomH := btnH + margin
		pbH := ih - y - bottomH
		if pbH < 80 {
			pbH = 80
		}
		pb.SetBounds(margin, y, contentW, pbH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	pb.SetOnPaint(func(c *wui.Canvas) {
		s.pbW, s.pbH = c.Size()
		renderViz(c, s)
	})

	cmb.SetOnChange(func(_ int) {
		s.view = cmb.Items()[cmb.SelectedIndex()]
		s.scrollX, s.scrollY = 0, 0
		if s.view == "pianoroll" {
			centerPianoRoll(s)
		}
		pb.Paint()
	})

	patCmb.SetOnChange(func(i int) {
		if i < 0 {
			return
		}
		s.patternIdx = i
		if s.view == "pianoroll" {
			centerPianoRoll(s)
			s.scrollX = 0
			pb.Paint()
		}
	})

	btnZoomIn.SetOnClick(func() {
		if s.zoomX < 8 {
			s.zoomX *= 1.25
		}
		clampScroll(s)
		pb.Paint()
	})
	btnZoomOut.SetOnClick(func() {
		if s.zoomX > 0.15 {
			s.zoomX /= 1.25
		}
		clampScroll(s)
		pb.Paint()
	})
	btnFit.SetOnClick(func() {
		s.zoomX, s.zoomY = 1, 1
		s.scrollX, s.scrollY = 0, 0
		pb.Paint()
	})

	w.SetOnMouseWheel(func(x, y int, delta float64) {
		ticks := int(delta / 120)
		if ticks == 0 && delta != 0 {
			if delta > 0 {
				ticks = 1
			} else {
				ticks = -1
			}
		}
		s.scrollY -= ticks * 60
		clampScroll(s)
		pb.Paint()
	})

	w.SetOnKeyDown(func(key int) {
		step := 40
		switch key {
		case wui.KeyUp:
			s.scrollY -= step
		case wui.KeyDown:
			s.scrollY += step
		case wui.KeyLeft:
			s.scrollX -= step
		case wui.KeyRight:
			s.scrollX += step
		case wui.KeyPrior:
			s.scrollY -= s.vpH
		case wui.KeyNext:
			s.scrollY += s.vpH
		case wui.KeyHome:
			s.scrollY = 0
			s.scrollX = 0
		case wui.KeyEnd:
			s.scrollY = 1 << 30
		case wui.KeyAdd, wui.KeyOEMPlus:
			if s.zoomX < 8 {
				s.zoomX *= 1.25
			}
			clampScroll(s)
		case wui.KeySubtract, wui.KeyOEMMinus:
			if s.zoomX > 0.15 {
				s.zoomX /= 1.25
			}
			clampScroll(s)
		default:
			return
		}
		clampScroll(s)
		pb.Paint()
	})

	w.SetOnMouseDown(func(_ wui.MouseButton, x, y int) {
		px, py := pb.Position()
		lx, ly := x-px, y-py
		if handleScrollClick(s, lx, ly) {
			pb.Paint()
		}
	})
	w.SetOnMouseUp(func(_ wui.MouseButton, x, y int) {
		s.dragMode = 0
	})
	pb.SetOnMouseMove(func(x, y int) {
		if s.dragMode == 0 {
			return
		}
		if handleScrollDrag(s, x, y) {
			pb.Paint()
		}
	})

	btnExportMIDI.SetOnClick(func() {
		if s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
			return
		}
		if path := browseSaveMIDI(w, "Export pattern as MIDI"); path != "" {
			if err := exportPatternMIDI(s.project, s.patternIdx, path); err != nil {
				wui.MessageBoxError("Export MIDI", "Failed to export:\n"+err.Error())
			} else {
				wui.MessageBoxInfo("Export MIDI", "Exported pattern to:\n"+path)
			}
		}
	})

	showModal(w)
}

func centerPianoRoll(s *vizState) {
	if s.project == nil || s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
		return
	}
	pat := s.project.Patterns[s.patternIdx]
	keyH := int(baseKeyH * s.zoomY)
	if keyH < 4 {
		keyH = 4
	}
	if len(pat.Notes) == 0 {
		s.scrollY = 60 * keyH
	} else {
		minK, maxK := 127, 0
		for _, n := range pat.Notes {
			k := int(n.Key)
			if k < minK {
				minK = k
			}
			if k > maxK {
				maxK = k
			}
		}
		centerK := (minK + maxK) / 2
		yOfKey := (127 - centerK) * keyH
		s.scrollY = yOfKey - (s.vpH-rulerH)/2
	}
	clampScroll(s)
}

func clampScroll(s *vizState) {
	maxY := s.contentH - s.vpH
	if maxY < 0 {
		maxY = 0
	}
	if s.scrollY < 0 {
		s.scrollY = 0
	}
	if s.scrollY > maxY {
		s.scrollY = maxY
	}
	maxX := s.contentW - s.vpW
	if maxX < 0 {
		maxX = 0
	}
	if s.scrollX < 0 {
		s.scrollX = 0
	}
	if s.scrollX > maxX {
		s.scrollX = maxX
	}
}

func vThumb(s *vizState) (pos, size int) {
	trackH := s.pbH - vizHeaderH - scrollbarSize
	if s.contentH <= s.vpH || trackH <= 0 {
		return 0, trackH
	}
	thumbH := s.vpH * trackH / s.contentH
	if thumbH < 24 {
		thumbH = 24
	}
	if thumbH > trackH {
		thumbH = trackH
	}
	maxScroll := s.contentH - s.vpH
	thumbY := 0
	if maxScroll > 0 {
		thumbY = (trackH - thumbH) * s.scrollY / maxScroll
	}
	return thumbY, thumbH
}

func hThumb(s *vizState) (pos, size int) {
	trackW := s.pbW - scrollbarSize
	if s.contentW <= s.vpW || trackW <= 0 {
		return 0, trackW
	}
	thumbW := s.vpW * trackW / s.contentW
	if thumbW < 24 {
		thumbW = 24
	}
	if thumbW > trackW {
		thumbW = trackW
	}
	maxScroll := s.contentW - s.vpW
	thumbX := 0
	if maxScroll > 0 {
		thumbX = (trackW - thumbW) * s.scrollX / maxScroll
	}
	return thumbX, thumbW
}

func handleScrollClick(s *vizState, lx, ly int) bool {
	vx := s.pbW - scrollbarSize
	if lx >= vx && lx < s.pbW && ly >= vizHeaderH && ly < s.pbH-scrollbarSize {
		pos, size := vThumb(s)
		relY := ly - vizHeaderH
		if relY >= pos && relY < pos+size {
			s.dragMode = 1
			s.dragStartY = ly
			s.dragStartScrollY = s.scrollY
		} else if s.contentH > s.vpH {
			if relY < pos {
				s.scrollY -= s.vpH
			} else {
				s.scrollY += s.vpH
			}
			clampScroll(s)
		}
		return true
	}
	hy := s.pbH - scrollbarSize
	if ly >= hy && ly < s.pbH && lx >= 0 && lx < s.pbW-scrollbarSize {
		pos, size := hThumb(s)
		if lx >= pos && lx < pos+size {
			s.dragMode = 2
			s.dragStartX = lx
			s.dragStartScrollX = s.scrollX
		} else if s.contentW > s.vpW {
			if lx < pos {
				s.scrollX -= s.vpW
			} else {
				s.scrollX += s.vpW
			}
			clampScroll(s)
		}
		return true
	}
	return false
}

func handleScrollDrag(s *vizState, lx, ly int) bool {
	switch s.dragMode {
	case 1:
		_, size := vThumb(s)
		delta := ly - s.dragStartY
		trackH := s.pbH - vizHeaderH - scrollbarSize
		trackRange := trackH - size
		maxScroll := s.contentH - s.vpH
		if trackRange > 0 && maxScroll > 0 {
			s.scrollY = s.dragStartScrollY + delta*maxScroll/trackRange
		}
		clampScroll(s)
		return true
	case 2:
		_, size := hThumb(s)
		delta := lx - s.dragStartX
		trackW := s.pbW - scrollbarSize
		trackRange := trackW - size
		maxScroll := s.contentW - s.vpW
		if trackRange > 0 && maxScroll > 0 {
			s.scrollX = s.dragStartScrollX + delta*maxScroll/trackRange
		}
		clampScroll(s)
		return true
	}
	return false
}

func measureContent(s *vizState) (int, int) {
	p := s.project
	const rowH = 24
	switch s.view {
	case "channels":
		return s.vpW, len(p.Channels)*rowH + 40
	case "patterns":
		return s.vpW, len(p.Patterns)*rowH + 40
	case "mixer":
		maxSlots := 0
		for _, ins := range p.Inserts {
			if len(ins.Slots) > maxSlots {
				maxSlots = len(ins.Slots)
			}
		}
		w := 380 + maxSlots*24 + 40
		if w < s.vpW {
			w = s.vpW
		}
		return w, len(p.Inserts)*rowH + 40
	case "pianoroll":
		keyH := int(baseKeyH * s.zoomY)
		if keyH < 4 {
			keyH = 4
		}
		h := rulerH + 128*keyH + 20
		w := s.vpW
		if s.patternIdx >= 0 && s.patternIdx < len(p.Patterns) {
			pat := p.Patterns[s.patternIdx]
			maxTick := uint32(0)
			for _, n := range pat.Notes {
				if e := n.Position + n.Length; e > maxTick {
					maxTick = e
				}
			}
			if maxTick < 384 {
				maxTick = 384
			}
			w = pianoKeysW + int(float64(maxTick)*basePxPerTick*s.zoomX) + 60
			if w < s.vpW {
				w = s.vpW
			}
		}
		return w, h
	default:
		ppq := 96
		if p.Header.PPQ > 0 {
			ppq = int(p.Header.PPQ)
		}
		barTicks := float64(ppq) * 4.0
		maxEnd := uint32(0)
		for _, a := range p.Arrangements {
			for _, cl := range a.Clips {
				if e := cl.Position + cl.Length; e > maxEnd {
					maxEnd = e
				}
			}
		}
		minTicks := uint32(barTicks * 8)
		if maxEnd < minTicks {
			maxEnd = minTicks
		}
		w := trackHdrW + int(float64(maxEnd)*basePxPerTick*s.zoomX) + 60
		if w < s.vpW {
			w = s.vpW
		}

		trackH := int(baseTrackH * s.zoomY)
		if trackH < 8 {
			trackH = 8
		}
		h := rulerH
		for _, a := range p.Arrangements {
			rows := len(a.Tracks)
			if rows == 0 {
				for _, cl := range a.Clips {
					if idx := 499 - cl.TrackRvidx; idx+1 > rows {
						rows = idx + 1
					}
				}
			}
			h += 22 + rows*trackH + 10
		}
		if h < s.vpH {
			h = s.vpH
		}
		return w, h
	}
}

func renderViz(c *wui.Canvas, s *vizState) {
	w, h := c.Size()
	c.FillRect(0, 0, w, h, colCanvasBG)

	c.FillRect(0, 0, w, vizHeaderH, colHeader)
	c.TextOut(14, 8, filepath.Base(s.source), colHeaderFG)
	c.TextOut(w-140, 26, fmt.Sprintf("Zoom %.0f%%", s.zoomX*100), colHeaderDi)

	vpW, vpH := w-scrollbarSize, h-vizHeaderH-scrollbarSize
	s.vpW, s.vpH = vpW, vpH
	s.contentW, s.contentH = measureContent(s)
	clampScroll(s)

	c.PushDrawRegion(0, vizHeaderH, vpW, vpH)
	switch s.view {
	case "channels":
		drawChannelsView(c, s)
	case "patterns":
		drawPatternsView(c, s)
	case "mixer":
		drawMixerView(c, s)
	case "pianoroll":
		drawPianoRoll(c, s)
	default:
		drawArrangementView(c, s)
	}
	c.PopDrawRegion()

	drawScrollbars(c, w, h, s)
}

func drawScrollbars(c *wui.Canvas, w, h int, s *vizState) {
	trackX := w - scrollbarSize
	trackY := vizHeaderH
	trackH := h - vizHeaderH - scrollbarSize
	c.FillRect(trackX, trackY, scrollbarSize, trackH, colTrack)
	if s.contentH > s.vpH {
		pos, size := vThumb(s)
		c.FillRect(trackX+2, trackY+pos, scrollbarSize-4, size, colThumb)
	}

	hTrackY := h - scrollbarSize
	hTrackW := w - scrollbarSize
	c.FillRect(0, hTrackY, hTrackW, scrollbarSize, colTrack)
	if s.contentW > s.vpW {
		pos, size := hThumb(s)
		c.FillRect(pos+2, hTrackY+2, size-4, scrollbarSize-4, colThumb)
	}

	c.FillRect(trackX, hTrackY, scrollbarSize, scrollbarSize, colHeader)
}

func drawArrangementView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	ppq := 96
	if p.Header.PPQ > 0 {
		ppq = int(p.Header.PPQ)
	}
	barTicks := float64(ppq) * 4.0
	beatTicks := float64(ppq)
	pxPerTick := basePxPerTick * s.zoomX
	trackH := int(baseTrackH * s.zoomY)
	if trackH < 8 {
		trackH = 8
	}

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1
	startBeat := int(startTick/beatTicks) - 1
	endBeat := int(endTick/beatTicks) + 1

	// Canvas background
	c.FillRect(trackHdrW, contentY+rulerH, s.vpW-trackHdrW, s.vpH-rulerH, colCanvasBG)

	// Ruler
	c.FillRect(trackHdrW, contentY, s.vpW-trackHdrW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := trackHdrW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < trackHdrW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+5, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	c.FillRect(0, contentY, trackHdrW, rulerH, colSidebar)
	c.Line(0, contentY+rulerH-1, trackHdrW, contentY+rulerH-1, colSep)

	cy := contentY + rulerH - s.scrollY
	for _, a := range p.Arrangements {
		if cy+22 > contentY+rulerH && cy < contentY+s.vpH {
			c.FillRect(0, cy, trackHdrW, 22, colSidebar)
			arrName := fmt.Sprintf("#%d", a.ID)
			if a.Name != nil && *a.Name != "" {
				arrName = *a.Name
			}
			c.TextOut(8, cy+4, "Arrangement "+arrName, colHeaderFG)
			c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, 22, colSidebar)
			c.Line(0, cy+21, s.vpW, cy+21, colSep)
		}
		cy += 22

		rows := len(a.Tracks)
		if rows == 0 {
			for _, cl := range a.Clips {
				if idx := 499 - cl.TrackRvidx; idx+1 > rows {
					rows = idx + 1
				}
			}
		}

		byTrack := map[int][]flp.Clip{}
		for _, cl := range a.Clips {
			byTrack[499-cl.TrackRvidx] = append(byTrack[499-cl.TrackRvidx], cl)
		}

		for t := 0; t < rows; t++ {
			visible := cy+trackH > contentY+rulerH && cy < contentY+s.vpH
			if visible {
				// Track header background
				hdrBG := wui.RGB(238, 240, 244)
				if t%2 == 1 {
					hdrBG = wui.RGB(232, 235, 240)
				}
				c.FillRect(0, cy, trackHdrW, trackH, hdrBG)
				if t < len(a.Tracks) {
					if tc, ok := rgbaToWuiColor(a.Tracks[t].Color); ok {
						c.FillRect(0, cy, 4, trackH, tc)
					}
				}
				label := fmt.Sprintf("Track %d", t+1)
				if t < len(a.Tracks) && a.Tracks[t].Name != nil && *a.Tracks[t].Name != "" {
					label = *a.Tracks[t].Name
				}
				c.TextOut(10, cy+(trackH-14)/2, truncate(label, 20), colText)
				c.Line(0, cy+trackH-1, trackHdrW, cy+trackH-1, colSep)

				// Track row background
				bg := colTrackBG
				if t%2 == 1 {
					bg = colTrackAlt
				}
				c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, trackH, bg)

				// Beat grid drawn on top of the row
				for beat := startBeat; beat <= endBeat; beat++ {
					bx := trackHdrW + int(float64(beat)*beatTicks*pxPerTick) - s.scrollX
					if bx < trackHdrW || bx > s.vpW {
						continue
					}
					col := colGridBeat
					if beat%4 == 0 {
						col = colGrid
					}
					c.Line(bx, cy, bx, cy+trackH, col)
				}
				c.Line(trackHdrW, cy+trackH-1, s.vpW, cy+trackH-1, colGrid)

				// Clips
				for _, cl := range byTrack[t] {
					cx := trackHdrW + int(float64(cl.Position)*pxPerTick) - s.scrollX
					cw := int(float64(cl.Length) * pxPerTick)
					if cw < 3 {
						cw = 3
					}
					if cx+cw < trackHdrW || cx > s.vpW {
						continue
					}
					if cx < trackHdrW {
						cw -= trackHdrW - cx
						cx = trackHdrW
					}
					if cx+cw > s.vpW {
						cw = s.vpW - cx
					}

					c.FillRect(cx, cy+2, cw, trackH-4, clipColorFor(p, cl))
					c.DrawRect(cx, cy+2, cw, trackH-4, colClipEdge)
					if lbl := clipLabel(p, cl); lbl != "" && cw > 30 {
						c.TextOut(cx+4, cy+(trackH-14)/2, truncate(lbl, cw/7), wui.RGB(255, 255, 255))
					}
				}
			}
			cy += trackH
		}
		cy += 10
	}
}

func drawPianoRoll(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	if s.patternIdx < 0 || s.patternIdx >= len(p.Patterns) {
		return
	}
	pat := p.Patterns[s.patternIdx]

	ppq := 96
	if p.Header.PPQ > 0 {
		ppq = int(p.Header.PPQ)
	}
	barTicks := float64(ppq) * 4.0
	beatTicks := float64(ppq)
	pxPerTick := basePxPerTick * s.zoomX
	keyH := int(baseKeyH * s.zoomY)
	if keyH < 4 {
		keyH = 4
	}

	gridTop := contentY + rulerH
	gridBottom := contentY + s.vpH

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1

	// Ruler
	c.FillRect(pianoKeysW, contentY, s.vpW-pianoKeysW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := pianoKeysW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+5, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	c.FillRect(0, contentY, pianoKeysW, rulerH, colSidebar)
	c.Line(0, contentY+rulerH-1, s.vpW, contentY+rulerH-1, colSep)

	// Keys + row backgrounds
	for k := 127; k >= 0; k-- {
		y := gridTop + (127-k)*keyH - s.scrollY
		if y+keyH < gridTop || y > gridBottom {
			continue
		}

		black := isBlackKey(k)

		var keyCol wui.Color
		var rowBg wui.Color
		if black {
			keyCol = wui.RGB(60, 62, 70)
			rowBg = wui.RGB(242, 244, 248)
		} else {
			keyCol = wui.RGB(255, 255, 255)
			rowBg = wui.RGB(255, 255, 255)
			if k%12 == 0 {
				rowBg = wui.RGB(235, 240, 248)
			}
		}
		c.FillRect(0, y, pianoKeysW, keyH, keyCol)
		c.Line(0, y+keyH-1, pianoKeysW, y+keyH-1, wui.RGB(180, 184, 190))
		c.FillRect(pianoKeysW, y, s.vpW-pianoKeysW, keyH, rowBg)

		if k%12 == 0 {
			c.TextOut(6, y+1, fmt.Sprintf("C%d", k/12-1), colTextDim)
		}
	}

	// Grid
	startBeat := int(startTick/beatTicks) - 1
	endBeat := int(endTick/beatTicks) + 1
	for beat := startBeat; beat <= endBeat; beat++ {
		bx := pianoKeysW + int(float64(beat)*beatTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		col := colGridBeat
		if beat%4 == 0 {
			col = colGrid
		}
		c.Line(bx, gridTop, bx, gridBottom, col)
	}

	// Notes
	for _, n := range pat.Notes {
		y := gridTop + (127-int(n.Key))*keyH - s.scrollY
		if y+keyH < gridTop || y > gridBottom {
			continue
		}
		x := pianoKeysW + int(float64(n.Position)*pxPerTick) - s.scrollX
		nw := int(float64(n.Length) * pxPerTick)
		if nw < 2 {
			nw = 2
		}
		if x+nw < pianoKeysW || x > s.vpW {
			continue
		}
		if x < pianoKeysW {
			nw -= pianoKeysW - x
			x = pianoKeysW
		}
		if x+nw > s.vpW {
			nw = s.vpW - x
		}

		col := noteColor(p, n.ChannelIid)
		c.FillRect(x, y+1, nw, keyH-2, col)
		c.DrawRect(x, y+1, nw, keyH-2, colClipEdge)
	}
}

func drawChannelsView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 24

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(14, cy, fmt.Sprintf("%d channels", len(p.Channels)), colText)
	}
	cy += 26

	for _, ch := range p.Channels {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			kind := string(ch.Kind)
			bg := colTrackBG
			c.FillRect(0, cy, s.vpW, rowH, bg)
			c.FillRect(0, cy, 5, rowH, channelColorFor(p, ch.Iid))
			c.Line(0, cy+rowH-1, s.vpW, cy+rowH-1, colGrid)

			name := fmt.Sprintf("#%d", ch.Iid)
			if ch.Name != nil && *ch.Name != "" {
				name = *ch.Name
			}
			c.TextOut(12, cy+4, truncate(name, 32), colText)
			c.TextOut(260, cy+4, kind, colTextDim)
			if ch.Plugin != nil {
				plug := ch.Plugin.InternalName
				if ch.Plugin.Name != nil && *ch.Plugin.Name != "" {
					plug = *ch.Plugin.Name
				}
				c.TextOut(380, cy+4, truncate(plug, 40), colTextDim)
			}
		}
		cy += rowH
	}
}

func drawPatternsView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 24

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(14, cy, fmt.Sprintf("%d patterns", len(p.Patterns)), colText)
	}
	cy += 26

	for _, pt := range p.Patterns {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			pc := patternColorFor(p, pt.ID)
			c.FillRect(0, cy, s.vpW, rowH, colTrackBG)
			c.FillRect(0, cy, 5, rowH, pc)
			c.Line(0, cy+rowH-1, s.vpW, cy+rowH-1, colGrid)

			name := fmt.Sprintf("#%d", pt.ID)
			if pt.Name != nil && *pt.Name != "" {
				name = *pt.Name
			}
			c.TextOut(12, cy+4, truncate(name, 30), colText)

			info := fmt.Sprintf("%d notes", len(pt.Notes))
			if n := len(pt.Controllers); n > 0 {
				info += fmt.Sprintf(", %d keyframes", n)
			}
			c.TextOut(300, cy+4, info, colTextDim)

			if len(pt.Notes) > 0 {
				bars := 80
				density := make([]int, bars)
				maxTick := uint32(1)
				for _, n := range pt.Notes {
					if n.Position+n.Length > maxTick {
						maxTick = n.Position + n.Length
					}
				}
				for _, n := range pt.Notes {
					i := int(uint64(n.Position) * uint64(bars) / uint64(maxTick))
					if i >= bars {
						i = bars - 1
					}
					density[i]++
				}
				maxD := 1
				for _, d := range density {
					if d > maxD {
						maxD = d
					}
				}
				bx := 480
				for i, d := range density {
					if d == 0 {
						continue
					}
					hh := (rowH - 8) * d / maxD
					if hh < 1 {
						hh = 1
					}
					c.FillRect(bx+i*3, cy+rowH-4-hh, 2, hh, pc)
				}
			}
		}
		cy += rowH
	}
}

func drawMixerView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 26

	offX := -s.scrollX

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(offX+14, cy, fmt.Sprintf("%d mixer inserts", len(p.Inserts)), colText)
	}
	cy += 26

	for _, ins := range p.Inserts {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			c.FillRect(offX, cy, s.contentW, rowH, colTrackBG)
			c.FillRect(offX, cy, 5, rowH, insertColorFor(p, ins.Index))
			c.Line(offX, cy+rowH-1, s.contentW, cy+rowH-1, colGrid)

			name := "(unnamed)"
			if ins.Name != nil && *ins.Name != "" {
				name = *ins.Name
			}
			c.TextOut(offX+12, cy+4, fmt.Sprintf("Insert %d", ins.Index), colTextDim)
			c.TextOut(offX+110, cy+4, truncate(name, 30), colText)

			sx := offX + 380
			for _, slot := range ins.Slots {
				label := "-"
				if slot.HasPlugin != nil && *slot.HasPlugin {
					switch {
					case slot.PluginVstName != nil:
						label = truncate(*slot.PluginVstName, 3)
					case slot.PluginName != nil:
						label = truncate(*slot.PluginName, 3)
					case slot.InternalName != nil:
						label = truncate(*slot.InternalName, 3)
					default:
						label = "*"
					}
				}
				bg := colTrackAlt
				if slot.HasPlugin != nil && *slot.HasPlugin {
					bg = insColor(ins.Index + slot.Index + 1)
				}
				c.FillRect(sx, cy+6, 22, rowH-12, bg)
				c.TextOut(sx+3, cy+6, label, colHeaderFG)
				sx += 24
			}
		}
		cy += rowH
	}
}

// ───────────────────────── viz helpers ─────────────────────────

func truncate(s string, n int) string {
	if n < 3 || len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func rgbaToWuiColor(c *flp.RGBA) (wui.Color, bool) {
	if c == nil {
		return wui.RGB(0, 0, 0), false
	}
	return wui.RGB(uint8(c.R), uint8(c.G), uint8(c.B)), true
}

func isBlackKey(k int) bool {
	switch k % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

func channelColor(kind string) wui.Color {
	switch kind {
	case "sampler":
		return wui.RGB(80, 160, 220)
	case "instrument":
		return wui.RGB(220, 140, 80)
	case "layer":
		return wui.RGB(180, 120, 200)
	case "automation":
		return wui.RGB(120, 200, 140)
	}
	return wui.RGB(140, 140, 140)
}

func patternColor(id int) wui.Color {
	h := float64(((id*137)%360)+360) / 60.0
	s := 0.65
	v := 0.90
	i := int(h)
	f := h - float64(i)
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return wui.RGB(uint8(r*255), uint8(g*255), uint8(b*255))
}

func insColor(idx int) wui.Color { return patternColor(idx + 7) }

func channelColorFor(p *flp.FLPProject, iid int) wui.Color {
	for _, ch := range p.Channels {
		if ch.Iid == iid {
			if col, ok := rgbaToWuiColor(ch.Color); ok {
				return col
			}
			return channelColor(string(ch.Kind))
		}
	}
	return channelColor("instrument")
}

func patternColorFor(p *flp.FLPProject, patternID int) wui.Color {
	for _, pt := range p.Patterns {
		if pt.ID == patternID {
			if col, ok := rgbaToWuiColor(pt.Color); ok {
				return col
			}
			break
		}
	}
	return patternColor(patternID)
}

func insertColorFor(p *flp.FLPProject, index int) wui.Color {
	for _, ins := range p.Inserts {
		if ins.Index == index {
			if col, ok := rgbaToWuiColor(ins.Color); ok {
				return col
			}
			break
		}
	}
	return insColor(index)
}

func noteColor(p *flp.FLPProject, channelIid int) wui.Color {
	for _, ch := range p.Channels {
		if ch.Iid == channelIid {
			if col, ok := rgbaToWuiColor(ch.Color); ok {
				return col
			}
			return channelColor(string(ch.Kind))
		}
	}
	return wui.RGB(150, 150, 150)
}

func clipColorFor(p *flp.FLPProject, cl flp.Clip) wui.Color {
	if cl.ItemIndex > 20480 {
		return patternColorFor(p, cl.ItemIndex-20480)
	}
	return channelColorFor(p, cl.ItemIndex)
}

func clipLabel(p *flp.FLPProject, cl flp.Clip) string {
	if cl.ItemIndex > 20480 {
		pid := cl.ItemIndex - 20480
		for _, pt := range p.Patterns {
			if pt.ID == pid {
				if pt.Name != nil && *pt.Name != "" {
					return *pt.Name
				}
				return fmt.Sprintf("Pattern %d", pid)
			}
		}
		return fmt.Sprintf("Pattern %d", pid)
	}
	for _, ch := range p.Channels {
		if ch.Iid == cl.ItemIndex {
			if ch.Name != nil && *ch.Name != "" {
				return *ch.Name
			}
			break
		}
	}
	return fmt.Sprintf("Clip %d", cl.ItemIndex)
}

// ───────────────────────── 5/6/7. Browsers ─────────────────────────

type browserRow struct {
	label   string
	details func() string
}

func openBrowser(name string, rows func(*flp.FLPProject) []browserRow) {
	if app.Project == nil {
		wui.MessageBoxInfo(name, "Please load a project first.")
		return
	}
	w := newModal(name)

	list := wui.NewStringList()
	if fontNormal != nil {
		list.SetFont(fontNormal)
	}
	w.Add(list)

	det := newOutput(0, 0, 100, 100)
	w.Add(det)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		bottomH := btnH + margin
		contentH := ih - y - bottomH
		if contentH < 80 {
			contentH = 80
		}

		listW := 280
		if listW > (iw-2*margin)/2 {
			listW = (iw - 2*margin) / 2
		}
		if listW < 150 {
			listW = 150
		}

		list.SetBounds(margin, y, listW, contentH)
		det.SetBounds(margin+listW+rowGap, y, iw-2*margin-listW-rowGap, contentH)
		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	data := rows(app.Project)
	items := make([]string, len(data))
	for i, r := range data {
		items[i] = r.label
	}
	list.SetItems(items)

	if len(items) > 0 {
		list.SetSelectedIndex(0)
		setText(det, data[0].details())
	} else {
		setText(det, "(empty)")
	}

	list.SetOnChange(func(i int) {
		if i >= 0 && i < len(data) {
			setText(det, data[i].details())
		}
	})

	showModal(w)
}

func openChannelTool(_ *wui.Window) {
	openBrowser("Channel Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Channels))
		for _, ch := range p.Channels {
			c := ch
			name := fmt.Sprintf("#%d", c.Iid)
			if c.Name != nil && *c.Name != "" {
				name = *c.Name
			}
			out = append(out, browserRow{
				label: name + " [" + string(c.Kind) + "]",
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Channel #%d\n", c.Iid)
					fmt.Fprintf(&b, "Kind: %s\n", c.Kind)
					if c.Name != nil && *c.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *c.Name)
					}
					if c.SamplePath != nil && *c.SamplePath != "" {
						fmt.Fprintf(&b, "Sample: %s\n", *c.SamplePath)
					}
					if c.Plugin != nil {
						fmt.Fprintf(&b, "Plugin: %s\n", c.Plugin.InternalName)
						if c.Plugin.Name != nil && *c.Plugin.Name != "" {
							fmt.Fprintf(&b, "  Name:   %s\n", *c.Plugin.Name)
						}
						if c.Plugin.Vendor != nil && *c.Plugin.Vendor != "" {
							fmt.Fprintf(&b, "  Vendor: %s\n", *c.Plugin.Vendor)
						}
					}
					if c.Levels != nil {
						fmt.Fprintf(&b, "Levels: pan=%d volume=%d\n", c.Levels.Pan, c.Levels.Volume)
					}
					if c.TargetInsert != nil {
						fmt.Fprintf(&b, "Routed to insert: %d\n", *c.TargetInsert)
					}
					if c.Color != nil {
						fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n", c.Color.R, c.Color.G, c.Color.B, c.Color.A)
					}
					if c.AutomationTarget != nil {
						fmt.Fprintf(&b, "Automation target: %s\n", c.AutomationTarget.Kind)
					}
					if n := len(c.AutomationPoints); n > 0 {
						fmt.Fprintf(&b, "Automation points: %d\n", n)
					}
					return b.String()
				},
			})
		}
		return out
	})
}

func openPatternTool(_ *wui.Window) {
	openBrowser("Pattern Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Patterns))
		for _, pt := range p.Patterns {
			pat := pt
			name := fmt.Sprintf("#%d", pat.ID)
			if pat.Name != nil && *pat.Name != "" {
				name = *pat.Name
			}
			out = append(out, browserRow{
				label: fmt.Sprintf("%s  (%d notes)", name, len(pat.Notes)),
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Pattern #%d\n", pat.ID)
					if pat.Name != nil && *pat.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *pat.Name)
					}
					if pat.Length != nil {
						fmt.Fprintf(&b, "Length: %d ticks\n", *pat.Length)
					}
					if pat.Looped != nil {
						fmt.Fprintf(&b, "Looped: %v\n", *pat.Looped)
					}
					fmt.Fprintf(&b, "Notes: %d\n", len(pat.Notes))
					fmt.Fprintf(&b, "Controllers: %d\n", len(pat.Controllers))
					if len(pat.Notes) > 0 {
						b.WriteString("\nFirst notes:\n")
						n := len(pat.Notes)
						if n > 20 {
							n = 20
						}
						for i := 0; i < n; i++ {
							note := pat.Notes[i]
							fmt.Fprintf(&b, "  pos=%-6d key=%-3d len=%-6d ch=%d vel=%d\n",
								note.Position, note.Key, note.Length, note.ChannelIid, note.Velocity)
						}
						if len(pat.Notes) > n {
							fmt.Fprintf(&b, "  ... and %d more\n", len(pat.Notes)-n)
						}
					}
					return b.String()
				},
			})
		}
		return out
	})
}

func openMixerTool(_ *wui.Window) {
	openBrowser("Mixer Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Inserts))
		for _, ins := range p.Inserts {
			i := ins
			name := "(unnamed)"
			if i.Name != nil && *i.Name != "" {
				name = *i.Name
			}
			out = append(out, browserRow{
				label: fmt.Sprintf("Insert %d  %s  (%d slots)", i.Index, name, len(i.Slots)),
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Insert %d\n", i.Index)
					if i.Name != nil && *i.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *i.Name)
					}
					if i.Flags != nil {
						fmt.Fprintf(&b, "Flags: enabled=%v locked=%v solo=%v\n",
							i.Flags.Enabled, i.Flags.Locked, i.Flags.Solo)
					}
					if i.Pan != nil {
						fmt.Fprintf(&b, "Pan: %d\n", *i.Pan)
					}
					if i.Volume != nil {
						fmt.Fprintf(&b, "Volume: %d\n", *i.Volume)
					}
					if i.Color != nil {
						fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n", i.Color.R, i.Color.G, i.Color.B, i.Color.A)
					}
					fmt.Fprintf(&b, "Slots: %d\n", len(i.Slots))
					for _, sl := range i.Slots {
						lbl := "(empty)"
						if sl.HasPlugin != nil && *sl.HasPlugin {
							switch {
							case sl.PluginVstName != nil:
								lbl = *sl.PluginVstName
							case sl.PluginName != nil:
								lbl = *sl.PluginName
							case sl.InternalName != nil:
								lbl = *sl.InternalName
							default:
								lbl = "(unknown)"
							}
						}
						fmt.Fprintf(&b, "  Slot %d: %s\n", sl.Index, lbl)
					}
					return b.String()
				},
			})
		}
		return out
	})
}

// ───────────────────────── 8. Git ─────────────────────────

func openGitTool(_ *wui.Window) {
	w := newModal("Git Integration")

	b1 := newBtn("Setup (local)", 0, 0, 150, btnH, nil)
	w.Add(b1)
	b2 := newBtn("Setup (global)", 0, 0, 150, btnH, nil)
	w.Add(b2)
	b3 := newBtn("Setup (textconv)", 0, 0, 150, btnH, nil)
	w.Add(b3)
	b4 := newBtn("Verify", 0, 0, 122, btnH, nil)
	w.Add(b4)

	out := newOutput(0, 0, 100, 100)
	setText(out, "Choose an action. Setup writes git config for the current repository.")
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		bw := (contentW - 3*rowInnerGap) / 4
		if bw < 90 {
			bw = 90
		}
		b1.SetBounds(margin, y, bw, btnH)
		b2.SetBounds(margin+bw+rowInnerGap, y, bw, btnH)
		b3.SetBounds(margin+2*(bw+rowInnerGap), y, bw, btnH)
		b4.SetBounds(margin+3*(bw+rowInnerGap), y, bw, btnH)
		y += btnH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, contentW, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	runSetup := func(o flp.SetupOptions) {
		res, err := flp.SetupGit(o)
		if err != nil {
			setText(out, "Error: "+err.Error())
			return
		}
		setText(out, flp.RenderSetupRecap(res))
	}
	b1.SetOnClick(func() { runSetup(flp.SetupOptions{}) })
	b2.SetOnClick(func() { runSetup(flp.SetupOptions{Scope: flp.ScopeGlobal}) })
	b3.SetOnClick(func() { runSetup(flp.SetupOptions{Mode: flp.ModeTextconv}) })
	b4.SetOnClick(func() { setText(out, flp.RenderVerifyReport(flp.VerifyGit(""))) })

	showModal(w)
}

// ───────────────────────── 9. About ─────────────────────────

func openAboutTool(_ *wui.Window) {
	w := newModal("About")

	lines := []string{
		"FLP Tool",
		"Version " + version,
		"",
		"A small toolbox for FL Studio (.flp) project files:",
		"semantic diff, inspection, visualisation, editing, git setup.",
		"",
		"Built with the flp library and gonutz/wui.",
	}
	labels := make([]*wui.Label, len(lines))
	for i, l := range lines {
		lbl := wui.NewLabel()
		lbl.SetText(l)
		if i == 0 && fontTitle != nil {
			lbl.SetFont(fontTitle)
		} else if fontNormal != nil {
			lbl.SetFont(fontNormal)
		}
		w.Add(lbl)
		labels[i] = lbl
	}

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		lineH := 24
		totalH := len(lines) * lineH
		startY := (ih - totalH) / 2
		if startY < margin {
			startY = margin
		}
		for i, lbl := range labels {
			lbl.SetBounds(margin*2, startY+i*lineH, iw-4*margin, lineH)
		}
		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	showModal(w)
}