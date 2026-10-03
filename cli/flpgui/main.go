// Command flp-gui is a desktop toolbox for FL Studio (.flp) project files.
//
// Fixed 640x430 windows. Every tool opens as a modal.
// Features: semantic diff, project inspector, visualiser (with scroll),
// channel / pattern / mixer browsers, git integration, and an editor.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/2dprototype/flp"
	"github.com/gonutz/wui/v2"
)

// ───────────────────────── constants ─────────────────────────

const (
	winW    = 640
	winH    = 430
	version = "0.1.2"

	vizHeaderH    = 44
	scrollbarSize = 12
	rulerH        = 22
	trackHdrW     = 140
	pianoKeysW    = 54

	basePxPerTick = 0.15 // pixels-per-tick at zoomX = 1
	baseTrackH    = 22   // track row height at zoomY = 1
	baseKeyH      = 12   // piano key height at zoomY = 1
)

// Palette.
var (
	colBG       = wui.RGB(245, 246, 248)
	colHeader   = wui.RGB(30, 34, 42)
	colHeaderFG = wui.RGB(240, 244, 248)
	colHeaderDi = wui.RGB(170, 180, 195)
	colCanvasBG = wui.RGB(22, 26, 32)
	colTrackBG  = wui.RGB(34, 38, 46)
	colTrackAlt = wui.RGB(40, 44, 52)
	colClipBG   = wui.RGB(58, 64, 76)
	colClipEdge = wui.RGB(90, 98, 112)
	colText     = wui.RGB(220, 224, 230)
	colTextDim  = wui.RGB(140, 148, 160)
	colTrack    = wui.RGB(34, 38, 46)
	colThumb    = wui.RGB(90, 98, 112)
	colThumbHi  = wui.RGB(130, 140, 158)
)

// Fonts are created once. nil means "use the system default".
var (
	fontNormal *wui.Font
	fontBold   *wui.Font
	fontTitle  *wui.Font
)

func makeFont(name string, px int, bold bool) *wui.Font {
	f, err := wui.NewFont(wui.FontDesc{
		Name:   name,
		Height: -px,
		Bold:   bold,
	})
	if err != nil {
		return nil
	}
	return f
}

func init() {
	fontNormal = makeFont("Segoe UI", 12, false)
	fontBold = makeFont("Segoe UI", 12, true)
	fontTitle = makeFont("Segoe UI", 16, true)
}

// ───────────────────────── text helpers ─────────────────────────

// setText writes to a TextEdit after converting Unix newlines to the CRLF
// that Win32 edit controls expect.
func setText(t *wui.TextEdit, text string) {
	windowsText := strings.ReplaceAll(text, "\n", "\r\n")
	t.SetText(windowsText)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// ───────────────────────── control factories ─────────────────────────

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

// ───────────────────────── main window ─────────────────────────

func main() { newMainWindow().Show() }

func newMainWindow() *wui.Window {
	w := wui.NewWindow()
	w.SetTitle("FLP Studio Tool")
	w.SetSize(winW, winH)
	w.SetResizable(false)
	w.SetBackground(colBG)
	w.SetHasMinButton(false)
	w.SetHasMaxButton(false)

	title := wui.NewLabel()
	title.SetText("FLP Studio Tool")
	title.SetBounds(20, 10, winW-40, 28)
	if fontTitle != nil {
		title.SetFont(fontTitle)
	}
	w.Add(title)

	w.Add(newLabel("Tools for FL Studio .flp project files", 20, 42, winW-40, 18))

	tools := []struct {
		label string
		open  func(*wui.Window)
	}{
		{"Diff Two Projects", openDiffTool},
		{"Edit & Save", openEditTool},
		{"Inspect Project", openInspectTool},
		{"Visualize Project", openVisualizerTool},
		{"Channel Browser", openChannelTool},
		{"Pattern Browser", openPatternTool},
		{"Mixer Browser", openMixerTool},
		{"Git Integration", openGitTool},
		{"About", openAboutTool},
	}

	const (
		btnW = 292
		btnH = 46
		gapX = 8
		gapY = 6
		colX = 16
		rowY = 68
	)
	for i, t := range tools {
		row, col := i/2, i%2
		open := t.open
		b := newBtn(t.label, colX+col*(btnW+gapX), rowY+row*(btnH+gapY), btnW, btnH,
			func() { open(w) })
		w.Add(b)
	}

	w.Add(newLabel("Version "+version, 20, winH-40, winW-40, 20))
	return w
}

// ───────────────────────── modal scaffolding ─────────────────────────

func newModal(title string) *wui.Window {
	w := wui.NewWindow()
	w.SetTitle(title)
	w.SetSize(winW, winH)
	w.SetResizable(false)
	w.SetBackground(colBG)
	w.SetHasMinButton(false)
	w.SetHasMaxButton(false)
	return w
}

func showModal(w *wui.Window) {
	if err := w.ShowModal(); err != nil {
		fmt.Fprintln(os.Stderr, "flp-gui:", err)
	}
}

func fileRow(w *wui.Window, y int, title string) *wui.EditLine {
	w.Add(newLabel("File:", 12, y+4, 50, 20))
	ed := newEdit(66, y, 430, 24)
	w.Add(ed)
	btn := newBtn("Browse...", 502, y, 106, 24, nil)
	btn.SetOnClick(func() {
		if p := browseFLP(w, title); p != "" {
			ed.SetText(p)
		}
	})
	w.Add(btn)
	return ed
}

func closeButton(w *wui.Window) *wui.Button {
	return newBtn("Close", 502, winH-56, 106, 26, func() { w.Close() })
}

func browseFLP(parent *wui.Window, title string) string {
	dlg := wui.NewFileOpenDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp, *.fst)", "flp", "fst")
	dlg.AddFilter("All files", "*.*")
	ok, path := dlg.ExecuteSingleSelection(parent)
	if !ok {
		return ""
	}
	return path
}

func browseSave(parent *wui.Window, title string) string {
	dlg := wui.NewFileSaveDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp)", "flp")
	dlg.AddFilter("All files", "*.*")
	ok, path := dlg.Execute(parent)
	if !ok {
		return ""
	}
	return path
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

// ───────────────────────── 1. Diff ─────────────────────────

func openDiffTool(_ *wui.Window) {
	w := newModal("Diff Two Projects")

	edA := fileRow(w, 12, "Select project A")
	edB := fileRow(w, 42, "Select project B")

	btnDiff := newBtn("Diff", 12, 74, 100, 26, nil)
	w.Add(btnDiff)
	btnVerbose := newBtn("Verbose", 118, 74, 100, 26, nil)
	w.Add(btnVerbose)
	btnClear := newBtn("Clear", 224, 74, 100, 26, nil)
	w.Add(btnClear)

	out := newOutput(12, 108, winW-40, winH-190)
	setText(out, "Pick two .flp files and press Diff or Verbose.")
	w.Add(out)

	run := func(verbose bool) {
		a, b := edA.Text(), edB.Text()
		if a == "" || b == "" {
			setText(out, "Error: both File A and File B must be set.")
			return
		}
		pa, err := loadProject(a)
		if err != nil {
			setText(out, formatErr(err))
			return
		}
		pb, err := loadProject(b)
		if err != nil {
			setText(out, formatErr(err))
			return
		}
		res := flp.CompareProjects(pa, pb)
		t := fmt.Sprintf("%s vs %s", filepath.Base(a), filepath.Base(b))
		body := flp.RenderSummary(res, flp.RenderSummaryOptions{Title: t, Verbose: verbose})
		if !flp.DiffSummaryHasChanges(res.Summary) {
			body += "\n\nNo differences. The two projects are identical."
		}
		setText(out, trimForUI(body))
	}
	btnDiff.SetOnClick(func() { run(false) })
	btnVerbose.SetOnClick(func() { run(true) })
	btnClear.SetOnClick(func() { setText(out, "") })

	w.Add(closeButton(w))
	showModal(w)
}

// ───────────────────────── 2. Edit & Save ─────────────────────────

// editSession holds the in-memory project being edited and its paths.
type editSession struct {
	project  *flp.FLPProject
	original *flp.FLPProject
	path     string
	saveAs   string
}

// propField is one label+edit pair in the properties panel.
type propField struct {
	label *wui.Label
	edit  *wui.EditLine
}

// categoryDef describes one editable category.
//   - list(p)  → labels shown in the middle column
//   - read(p,i) → current values for item i, in the order of `props`
//   - apply(p,i,vals) → returns a new project with the values written
//   - msg(i,vals) → one-line log/status message
type categoryDef struct {
	label string
	list  func(*flp.FLPProject) []string
	read  func(*flp.FLPProject, int) []string
	props []string
	apply func(*flp.FLPProject, int, []string) (*flp.FLPProject, error)
	msg   func(int, []string) string
}

// ── string / value helpers ──

func parseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func fmtFloatShort(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
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

// arrTrackAt flattens "track k across all arrangements" back to (arrIdx, trackIdx).
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

// ── the category table ──

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
			vol, pan := "1", "0"
			if c.Levels != nil {
				vol = fmtFloatShort(float64(c.Levels.Volume) / 12800.0)
				pan = fmtFloatShort(float64(c.Levels.Pan) / 6400.0)
			}
			ti := "-1"
			if c.TargetInsert != nil {
				ti = strconv.Itoa(*c.TargetInsert)
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
			if cur, err = flp.SetChannelVolume(cur, iid, parseFloat(v[1])); err != nil {
				return nil, err
			}
			if cur, err = flp.SetChannelPan(cur, iid, parseFloat(v[2])); err != nil {
				return nil, err
			}
			if cur, err = flp.SetChannelRouting(cur, iid, parseInt(v[3])); err != nil {
				return nil, err
			}
			cur, err = flp.SetChannelColor(cur, iid, parseColorStrings(v[4:]))
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
			cur, err = flp.SetPatternColor(cur, pid, parseColorStrings(v[2:]))
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
			// SetInsertName accepts empty string (clears the name).
			if cur, err = flp.SetInsertName(cur, idx, v[0]); err != nil {
				return nil, err
			}
			cur, err = flp.SetInsertColor(cur, idx, parseColorStrings(v[1:]))
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
			grouped := "0"
			if t.Grouped != nil && *t.Grouped {
				grouped = "1"
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
			cur, err = flp.SetTrackColor(cur, arrID, ti, parseColorStrings(v[2:]))
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Track index %d updated", i)
		},
	},
}

// ── the modal itself ──

func openEditTool(_ *wui.Window) {
	w := newModal("Edit Project")

	// ── File row (custom: File + Browse + Load) ──
	w.Add(newLabel("File:", 12, 10, 50, 20))
	edSrc := newEdit(66, 6, 320, 24)
	w.Add(edSrc)
	btnBrowse := newBtn("Browse", 392, 6, 90, 24, nil)
	w.Add(btnBrowse)
	btnLoad := newBtn("Load", 488, 6, 120, 24, nil)
	w.Add(btnLoad)

	// ── Save row (Save to + Browse + Save) ──
	w.Add(newLabel("Save to:", 12, 40, 50, 20))
	edDst := newEdit(66, 36, 320, 24)
	w.Add(edDst)
	btnDstBrowse := newBtn("Browse", 392, 36, 90, 24, nil)
	w.Add(btnDstBrowse)
	btnSave := newBtn("Save", 488, 36, 120, 24, nil)
	w.Add(btnSave)

	// ── Column headers ──
	w.Add(newLabel("Category", 12, 66, 130, 16))
	w.Add(newLabel("Item", 150, 66, 170, 16))
	w.Add(newLabel("Properties", 326, 66, 300, 16))

	// ── Three-column split ──
	const splitY = 84
	const splitH = 256 // 8 rows × 32 px

	catList := wui.NewStringList()
	catList.SetBounds(8, splitY, 138, splitH)
	if fontNormal != nil {
		catList.SetFont(fontNormal)
	}
	w.Add(catList)

	itemList := wui.NewStringList()
	itemList.SetBounds(150, splitY, 168, splitH)
	if fontNormal != nil {
		itemList.SetFont(fontNormal)
	}
	w.Add(itemList)

	// ── Property panel (up to 8 rows of 32 px) ──
	const propX = 326
	const propW = 306
	const rowH = 32
	fields := make([]propField, 8)
	for i := range fields {
		y := splitY + i*rowH
		lbl := newLabel("", propX, y+6, 100, 20)
		ed := newEdit(propX+104, y+2, propW-104, 24)
		w.Add(lbl)
		w.Add(ed)
		fields[i] = propField{lbl, ed}
	}

	// ── Status bar + Revert / Apply ──
	status := newLabel("Load an .flp to begin.", 12, winH-60, 300, 20)
	w.Add(status)
	btnRevert := newBtn("Revert", 318, winH-62, 90, 26, nil)
	w.Add(btnRevert)
	btnApply := newBtn("Apply", 414, winH-62, 90, 26, nil)
	w.Add(btnApply)

	w.Add(closeButton(w))

	// ── Session state ──
	session := &editSession{}
	catIndex := 0
	itemIndex := 0

	// ── Panel helpers ──
	clearFields := func() {
		for i := range fields {
			fields[i].label.SetText("")
			fields[i].edit.SetText("")
		}
	}
	populateFields := func(cat categoryDef, idx int) {
		clearFields()
		if session.project == nil {
			return
		}
		vals := cat.read(session.project, idx)
		for i, p := range cat.props {
			if i >= len(fields) {
				break
			}
			fields[i].label.SetText(p)
			if i < len(vals) {
				fields[i].edit.SetText(vals[i])
			}
		}
	}
	readFields := func(cat categoryDef) []string {
		out := make([]string, len(cat.props))
		for i := range out {
			if i < len(fields) {
				out[i] = fields[i].edit.Text()
			}
		}
		return out
	}
	refreshItems := func() {
		if session.project == nil || catIndex < 0 || catIndex >= len(editCategories) {
			itemList.SetItems([]string{})
			clearFields()
			return
		}
		cat := editCategories[catIndex]
		items := cat.list(session.project)
		itemList.SetItems(items)
		keep := itemIndex
		if keep < 0 || keep >= len(items) {
			keep = 0
		}
		if len(items) > 0 {
			itemList.SetSelectedIndex(keep)
			itemIndex = keep
			populateFields(cat, keep)
		} else {
			itemIndex = -1
			clearFields()
		}
	}
	updateStatus := func() {
		if session.project == nil {
			status.SetText("Load an .flp to begin.")
			return
		}
		src := filepath.Base(session.path)
		if session.saveAs != "" {
			src = filepath.Base(session.saveAs)
		}
		mod := ""
		if session.project != session.original {
			mod = "  •  edited"
		}
		status.SetText(fmt.Sprintf("%s  •  %d ch, %d pat, %d ins%s",
			src,
			len(session.project.Channels),
			len(session.project.Patterns),
			len(session.project.Inserts),
			mod))
	}

	// ── Category list ──
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

	// ── Item list ──
	itemList.SetOnChange(func(i int) {
		if i < 0 || catIndex < 0 || catIndex >= len(editCategories) {
			return
		}
		itemIndex = i
		populateFields(editCategories[catIndex], i)
	})

	// ── Load ──
	btnBrowse.SetOnClick(func() {
		if p := browseFLP(w, "Open FL Studio project"); p != "" {
			edSrc.SetText(p)
		}
	})
	btnLoad.SetOnClick(func() {
		path := strings.TrimSpace(edSrc.Text())
		if path == "" {
			status.SetText("No source file set.")
			return
		}
		p, err := loadProject(path)
		if err != nil {
			status.SetText("Load error: " + err.Error())
			return
		}
		session.project = p
		session.original = p
		session.path = path
		session.saveAs = ""
		if strings.TrimSpace(edDst.Text()) == "" {
			edDst.SetText(path)
		}
		catIndex = 0
		itemIndex = 0
		catList.SetSelectedIndex(0)
		refreshItems()
		updateStatus()
	})

	// ── Save ──
	doSave := func(path string) {
		if session.project == nil {
			status.SetText("No project to save.")
			return
		}
		if path == "" {
			status.SetText("No save path set.")
			return
		}
		data, err := flp.SerializeFLPProject(session.project)
		if err != nil {
			status.SetText("Serialize error: " + err.Error())
			return
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			status.SetText("Write error: " + err.Error())
			return
		}
		session.original = session.project
		session.path = path
		session.saveAs = path
		status.SetText(fmt.Sprintf("Saved %d bytes to %s", len(data), filepath.Base(path)))
		updateStatus()
	}
	btnDstBrowse.SetOnClick(func() {
		if p := browseSave(w, "Save edited project as"); p != "" {
			edDst.SetText(p)
		}
	})
	btnSave.SetOnClick(func() {
		p := strings.TrimSpace(edDst.Text())
		if p == "" {
			p = session.path
		}
		doSave(p)
	})

	// ── Apply ──
	btnApply.SetOnClick(func() {
		if session.project == nil {
			status.SetText("No project loaded.")
			return
		}
		if catIndex < 0 || catIndex >= len(editCategories) {
			return
		}
		if itemIndex < 0 {
			status.SetText("No item selected.")
			return
		}
		cat := editCategories[catIndex]
		vals := readFields(cat)
		next, err := cat.apply(session.project, itemIndex, vals)
		if err != nil {
			status.SetText("Apply failed: " + err.Error())
			return
		}
		session.project = next
		status.SetText(cat.msg(itemIndex, vals))
		refreshItems()
		updateStatus()
	})

	// ── Revert ──
	btnRevert.SetOnClick(func() {
		if session.original == nil {
			status.SetText("Nothing loaded to revert to.")
			return
		}
		session.project = session.original
		status.SetText("Reverted to last loaded/saved state.")
		refreshItems()
		updateStatus()
	})

	showModal(w)
}

// ───────────────────────── 3. Inspect ─────────────────────────

func openInspectTool(_ *wui.Window) {
	w := newModal("Inspect Project")

	ed := fileRow(w, 12, "Select a project")

	w.Add(newLabel("Format:", 12, 48, 60, 20))
	cmb := newCombo([]string{"text", "canonical", "json"}, 76, 46, 140, 24)
	w.Add(cmb)

	btnShow := newBtn("Show", 224, 45, 100, 26, nil)
	w.Add(btnShow)
	btnClear := newBtn("Clear", 330, 45, 100, 26, nil)
	w.Add(btnClear)

	out := newOutput(12, 80, winW-40, winH-162)
	setText(out, "Pick a project file and press Show.")
	w.Add(out)

	btnShow.SetOnClick(func() {
		path := ed.Text()
		if path == "" {
			setText(out, "Error: pick a file to inspect.")
			return
		}
		p, err := loadProject(path)
		if err != nil {
			setText(out, formatErr(err))
			return
		}
		switch cmb.Items()[cmb.SelectedIndex()] {
		case "canonical":
			setText(out, trimForUI(flp.RenderCanonical(p)))
		case "json":
			s, jerr := marshalIndentedJSON(flp.ToFlpInfoJson(p))
			if jerr != nil {
				setText(out, formatErr(jerr))
				return
			}
			setText(out, trimForUI(s))
		default:
			setText(out, flp.RenderInfo(p, path))
		}
	})
	btnClear.SetOnClick(func() { setText(out, "") })

	w.Add(closeButton(w))
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

	// Drag state: 0 none, 1 v-scrollbar, 2 h-scrollbar
	dragMode         int
	dragStartX       int
	dragStartY       int
	dragStartScrollX int
	dragStartScrollY int

	patternIdx int
}

func openVisualizerTool(_ *wui.Window) {
	w := newModal("Visualize Project")
	w.SetBackground(colCanvasBG)

	ed := fileRow(w, 12, "Select a project")

	// ── Toolbar row ──
	w.Add(newLabel("View:", 12, 50, 36, 20))
	cmb := newCombo([]string{"arrangement", "pianoroll", "channels", "patterns", "mixer"}, 50, 46, 110, 24)
	w.Add(cmb)

	w.Add(newLabel("Pattern:", 166, 50, 56, 20))
	patCmb := newCombo([]string{"(load a project)"}, 224, 46, 160, 24)
	w.Add(patCmb)

	w.Add(newLabel("Zoom:", 390, 50, 40, 20))
	btnZoomOut := newBtn("−", 432, 46, 26, 24, nil)
	w.Add(btnZoomOut)
	btnZoomIn := newBtn("+", 460, 46, 26, 24, nil)
	w.Add(btnZoomIn)
	btnFit := newBtn("Fit", 488, 46, 36, 24, nil)
	w.Add(btnFit)

	btnRender := newBtn("Render", 528, 46, 100, 24, nil)
	w.Add(btnRender)

	// ── Canvas ──
	pb := wui.NewPaintBox()
	pb.SetBounds(12, 80, winW-24, winH-152)
	w.Add(pb)

	s := &vizState{
		view:       "arrangement",
		zoomX:      1,
		zoomY:      1,
		patternIdx: 0,
	}

	pb.SetOnPaint(func(c *wui.Canvas) {
		s.pbW, s.pbH = c.Size()
		if s.project == nil {
			c.FillRect(0, 0, s.pbW, s.pbH, colCanvasBG)
			c.TextOut(16, 16, "No project loaded.", colTextDim)
			c.TextOut(16, 40, "Pick a .flp file and press Render.", colTextDim)
			return
		}
		renderViz(c, s)
	})

	btnRender.SetOnClick(func() {
		path := ed.Text()
		if path == "" {
			return
		}
		p, err := loadProject(path)
		if err != nil {
			s.project = nil
			s.source = path
			s.pbW, s.pbH = winW-24, winH-152
			s.contentH = 0
			s.contentW = 0
			pb.Paint()
			return
		}
		s.project = p
		s.source = path
		s.scrollX = 0
		s.scrollY = 0
		s.view = cmb.Items()[cmb.SelectedIndex()]
		s.patternIdx = 0

		patItems := make([]string, len(p.Patterns))
		for i, pt := range p.Patterns {
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
		if len(patItems) > 0 {
			patCmb.SetSelectedIndex(0)
		}
		pb.Paint()
	})

	cmb.SetOnChange(func(_ int) {
		s.view = cmb.Items()[cmb.SelectedIndex()]
		s.scrollX = 0
		s.scrollY = 0
		if s.view == "pianoroll" && s.project != nil {
			centerPianoRoll(s)
		}
		if s.project != nil {
			pb.Paint()
		}
	})

	patCmb.SetOnChange(func(i int) {
		if i < 0 {
			return
		}
		s.patternIdx = i
		if s.view == "pianoroll" && s.project != nil {
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
		if s.project != nil {
			pb.Paint()
		}
	})
	btnZoomOut.SetOnClick(func() {
		if s.zoomX > 0.15 {
			s.zoomX /= 1.25
		}
		clampScroll(s)
		if s.project != nil {
			pb.Paint()
		}
	})
	btnFit.SetOnClick(func() {
		s.zoomX = 1
		s.zoomY = 1
		s.scrollX = 0
		s.scrollY = 0
		if s.project != nil {
			pb.Paint()
		}
	})

	// Wheel: vertical scroll
	w.SetOnMouseWheel(func(x, y int, delta float64) {
		if s.project == nil {
			return
		}
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

	// Keyboard nav + zoom
	w.SetOnKeyDown(func(key int) {
		if s.project == nil {
			return
		}
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

	// Mouse: scrollbar drag
	const pbX, pbY = 12, 80
	w.SetOnMouseDown(func(_ wui.MouseButton, x, y int) {
		if s.project == nil {
			return
		}
		lx, ly := x-pbX, y-pbY
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

	w.Add(closeButton(w))
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
	// Vertical scrollbar
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
	// Horizontal scrollbar
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
			pxPerTick := basePxPerTick * s.zoomX
			w = pianoKeysW + int(float64(maxTick)*pxPerTick) + 60
			if w < s.vpW {
				w = s.vpW
			}
		}
		return w, h
	default: // arrangement
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
		pxPerTick := basePxPerTick * s.zoomX
		w := trackHdrW + int(float64(maxEnd)*pxPerTick) + 60
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
					idx := 499 - cl.TrackRvidx
					if idx+1 > rows {
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

	// Top info bar
	c.FillRect(0, 0, w, vizHeaderH, colHeader)
	tempo := 0.0
	if t := flp.GetTempo(s.project); t != nil {
		tempo = *t
	}
	c.TextOut(14, 6, filepath.Base(s.source), colHeaderFG)
	meta := fmt.Sprintf("%.1f BPM   PPQ %d   %d channels   %d patterns   %d inserts",
		tempo, s.project.Header.PPQ, len(s.project.Channels),
		len(s.project.Patterns), len(s.project.Inserts))
	c.TextOut(14, 24, meta, colHeaderDi)
	c.TextOut(w-140, 24, fmt.Sprintf("Zoom %.0f%%", s.zoomX*100), colHeaderDi)

	vpW := w - scrollbarSize
	vpH := h - vizHeaderH - scrollbarSize
	s.vpW = vpW
	s.vpH = vpH

	s.contentW, s.contentH = measureContent(s)
	clampScroll(s)

	// Clipped content area
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
	// Vertical track
	trackX := w - scrollbarSize
	trackY := vizHeaderH
	trackH := h - vizHeaderH - scrollbarSize
	c.FillRect(trackX, trackY, scrollbarSize, trackH, colTrack)
	if s.contentH > s.vpH {
		pos, size := vThumb(s)
		c.FillRect(trackX+2, trackY+pos, scrollbarSize-4, size, colThumb)
	}

	// Horizontal track
	hTrackY := h - scrollbarSize
	hTrackW := w - scrollbarSize
	c.FillRect(0, hTrackY, hTrackW, scrollbarSize, colTrack)
	if s.contentW > s.vpW {
		pos, size := hThumb(s)
		c.FillRect(pos+2, hTrackY+2, size-4, scrollbarSize-4, colThumb)
	}

	// Corner
	c.FillRect(trackX, hTrackY, scrollbarSize, scrollbarSize, colHeader)
}

// ── Arrangement ──

func drawArrangementView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH

	ppq := 96
	if p.Header.PPQ > 0 {
		ppq = int(p.Header.PPQ)
	}
	barTicks := float64(ppq) * 4.0
	pxPerTick := basePxPerTick * s.zoomX
	trackH := int(baseTrackH * s.zoomY)
	if trackH < 8 {
		trackH = 8
	}

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1

	// Ruler
	c.FillRect(trackHdrW, contentY, s.vpW-trackHdrW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := trackHdrW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < trackHdrW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+3, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	// Corner
	c.FillRect(0, contentY, trackHdrW, rulerH, colHeader)

	// Column backgrounds
	c.FillRect(0, contentY+rulerH, trackHdrW, s.vpH-rulerH, colTrackAlt)
	c.FillRect(trackHdrW, contentY+rulerH, s.vpW-trackHdrW, s.vpH-rulerH, colTrackBG)

	cy := contentY + rulerH - s.scrollY
	for _, a := range p.Arrangements {
		// Arrangement header row
		if cy+22 > contentY+rulerH && cy < contentY+s.vpH {
			c.FillRect(0, cy, trackHdrW, 22, colHeader)
			arrName := fmt.Sprintf("#%d", a.ID)
			if a.Name != nil && *a.Name != "" {
				arrName = *a.Name
			}
			c.TextOut(6, cy+3, "Arrangement "+arrName, colHeaderFG)
			c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, 22, colTrackAlt)
		}
		cy += 22

		rows := len(a.Tracks)
		if rows == 0 {
			for _, cl := range a.Clips {
				idx := 499 - cl.TrackRvidx
				if idx+1 > rows {
					rows = idx + 1
				}
			}
		}

		byTrack := map[int][]flp.Clip{}
		for _, cl := range a.Clips {
			idx := 499 - cl.TrackRvidx
			byTrack[idx] = append(byTrack[idx], cl)
		}

		for t := 0; t < rows; t++ {
			visible := cy+trackH > contentY+rulerH && cy < contentY+s.vpH
			if visible {
				bg := colTrackBG
				if t%2 == 1 {
					bg = colTrackAlt
				}

				// Track header (with FLP track color stripe, if any)
				c.FillRect(0, cy, trackHdrW, trackH, bg)
				if t < len(a.Tracks) {
					if tc, ok := rgbaToWuiColor(a.Tracks[t].Color); ok {
						c.FillRect(0, cy, 4, trackH, tc)
					}
				}
				label := fmt.Sprintf("Track %d", t+1)
				if t < len(a.Tracks) {
					if n := a.Tracks[t].Name; n != nil && *n != "" {
						label = *n
					}
				}
				c.TextOut(8, cy+(trackH-14)/2, truncate(label, 20), colTextDim)
				c.Line(0, cy+trackH-1, trackHdrW, cy+trackH-1, colCanvasBG)

				// Timeline row
				c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, trackH, bg)

				// Bar grid
				for bar := startBar; bar <= endBar; bar++ {
					bx := trackHdrW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
					if bx < trackHdrW || bx > s.vpW {
						continue
					}
					c.Line(bx, cy, bx, cy+trackH, colCanvasBG)
				}
				c.Line(trackHdrW, cy+trackH-1, s.vpW, cy+trackH-1, colCanvasBG)

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
						c.TextOut(cx+4, cy+(trackH-14)/2, truncate(lbl, cw/7), colHeaderFG)
					}
				}
			}
			cy += trackH
		}
		cy += 10
	}
}

// ── Piano roll ──

func drawPianoRoll(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH

	if s.patternIdx < 0 || s.patternIdx >= len(p.Patterns) {
		c.TextOut(16, contentY+16, "No pattern selected.", colTextDim)
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

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1

	gridTop := contentY + rulerH
	gridBottom := contentY + s.vpH

	// Ruler
	c.FillRect(pianoKeysW, contentY, s.vpW-pianoKeysW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := pianoKeysW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+3, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	// Corner (pattern label)
	c.FillRect(0, contentY, pianoKeysW, rulerH, colHeader)

	// Backgrounds
	c.FillRect(0, gridTop, pianoKeysW, s.vpH-rulerH, colTrackAlt)
	c.FillRect(pianoKeysW, gridTop, s.vpW-pianoKeysW, s.vpH-rulerH, colCanvasBG)

	// Piano key rows + grid
	for k := 127; k >= 0; k-- {
		y := gridTop + (127-k)*keyH - s.scrollY
		if y+keyH < gridTop || y > gridBottom {
			continue
		}
		black := isBlackKey(k)
		var keyCol wui.Color
		if black {
			keyCol = wui.RGB(40, 40, 48)
		} else {
			keyCol = wui.RGB(210, 210, 218)
		}
		c.FillRect(0, y, pianoKeysW, keyH, keyCol)
		c.Line(0, y+keyH-1, pianoKeysW, y+keyH-1, wui.RGB(80, 80, 90))

		var rowBg wui.Color
		if black {
			rowBg = wui.RGB(28, 30, 38)
		} else {
			rowBg = wui.RGB(34, 36, 44)
		}
		c.FillRect(pianoKeysW, y, s.vpW-pianoKeysW, keyH, rowBg)

		if k%12 == 0 {
			c.TextOut(4, y+1, fmt.Sprintf("C%d", k/12-1), wui.RGB(30, 30, 30))
		}
	}

	// Beat grid
	startBeat := int(startTick/beatTicks) - 1
	endBeat := int(endTick/beatTicks) + 1
	for beat := startBeat; beat <= endBeat; beat++ {
		bx := pianoKeysW + int(float64(beat)*beatTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		c.Line(bx, gridTop, bx, gridBottom, wui.RGB(42, 44, 52))
	}
	// Bar grid
	for bar := startBar; bar <= endBar; bar++ {
		bx := pianoKeysW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		c.Line(bx, gridTop, bx, gridBottom, wui.RGB(60, 64, 78))
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

func isBlackKey(k int) bool {
	switch k % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// noteColor returns the channel's stored color if any, else a kind-based color.
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

// ── Channels / Patterns / Mixer ──

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
			c.FillRect(0, cy, s.vpW, rowH, colTrackBG)
			c.FillRect(0, cy, 5, rowH, channelColorFor(p, ch.Iid))

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
				bg := colClipBG
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

// ── Color / text helpers ──

func truncate(s string, n int) string {
	if n < 3 || len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// rgbaToWuiColor converts a *flp.RGBA into a wui.Color. The bool is false
// when the source is nil (no explicit color stored in the FLP).
func rgbaToWuiColor(c *flp.RGBA) (wui.Color, bool) {
	if c == nil {
		return wui.RGB(0, 0, 0), false
	}
	return wui.RGB(uint8(c.R), uint8(c.G), uint8(c.B)), true
}

// channelColor is the fallback kind-based color used when an FLP channel
// has no explicit color.
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

// patternColor is the deterministic hash-based fallback used when an FLP
// pattern has no explicit color.
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

// channelColorFor prefers the FLP-stored channel color, else falls back to kind.
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

// patternColorFor prefers the FLP-stored pattern color, else hash fallback.
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

// insertColorFor prefers the FLP-stored insert color, else hash fallback.
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
	w := newModal(name)

	ed := fileRow(w, 12, "Select a project")

	btnLoad := newBtn("Load", 12, 44, 100, 26, nil)
	w.Add(btnLoad)

	list := wui.NewStringList()
	list.SetBounds(12, 78, 260, winH-146)
	if fontNormal != nil {
		list.SetFont(fontNormal)
	}
	w.Add(list)

	det := newOutput(280, 78, winW-292, winH-146)
	w.Add(det)

	var data []browserRow

	btnLoad.SetOnClick(func() {
		path := ed.Text()
		if path == "" {
			setText(det, "Error: pick a file to load.")
			return
		}
		p, err := loadProject(path)
		if err != nil {
			setText(det, formatErr(err))
			return
		}
		data = rows(p)
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
	})

	list.SetOnChange(func(i int) {
		if i >= 0 && i < len(data) {
			setText(det, data[i].details())
		}
	})

	w.Add(closeButton(w))
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

	b1 := newBtn("Setup (local)", 12, 12, 150, 28, nil)
	w.Add(b1)
	b2 := newBtn("Setup (global)", 170, 12, 150, 28, nil)
	w.Add(b2)
	b3 := newBtn("Setup (textconv)", 328, 12, 150, 28, nil)
	w.Add(b3)
	b4 := newBtn("Verify", 486, 12, 122, 28, nil)
	w.Add(b4)

	out := newOutput(12, 52, winW-24, winH-124)
	setText(out, "Choose an action. Setup writes git config for the current repository.")
	w.Add(out)

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

	w.Add(closeButton(w))
	showModal(w)
}

// ───────────────────────── 9. About ─────────────────────────

func openAboutTool(_ *wui.Window) {
	w := wui.NewWindow()
	w.SetTitle("About")
	w.SetSize(winW, winH)
	w.SetResizable(false)
	w.SetBackground(colBG)
	w.SetHasMinButton(false)
	w.SetHasMaxButton(false)

	lines := []string{
		"FLP Studio Tool",
		"Version " + version,
		"",
		"A small toolbox for FL Studio (.flp) project files:",
		"semantic diff, inspection, visualisation, editing, git setup.",
		"",
		"Built with the flp library and gonutz/wui.",
	}
	y := 24
	for i, l := range lines {
		lbl := wui.NewLabel()
		lbl.SetText(l)
		lbl.SetBounds(24, y, winW-48, 22)
		if i == 0 && fontTitle != nil {
			lbl.SetFont(fontTitle)
		} else if fontNormal != nil {
			lbl.SetFont(fontNormal)
		}
		w.Add(lbl)
		y += 24
	}

	w.Add(newBtn("Close", winW-130, winH-70, 106, 28, func() { w.Close() }))
	showModal(w)
}