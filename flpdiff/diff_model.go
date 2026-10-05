package flpdiff

import (
	"fmt"
	"strings"
)

// ChangeKind is "added" | "removed" | "modified".
type ChangeKind string

const (
	KindAdded    ChangeKind = "added"
	KindRemoved  ChangeKind = "removed"
	KindModified ChangeKind = "modified"
)

// NoteChangeKind is "added" | "removed" | "moved" | "modified".
type NoteChangeKind string

const (
	NoteAdded    NoteChangeKind = "added"
	NoteRemoved  NoteChangeKind = "removed"
	NoteMoved    NoteChangeKind = "moved"
	NoteModified NoteChangeKind = "modified"
)

// Change is a single property-level change anywhere in the canonical tree.
type Change struct {
	Path       string      `json:"path"`
	Kind       ChangeKind  `json:"kind"`
	OldValue   interface{} `json:"oldValue"`
	NewValue   interface{} `json:"newValue"`
	HumanLabel string      `json:"humanLabel"`
}

// MakeChange validates and returns a Change (panics on an empty label, mirroring the TS throw).
func MakeChange(c Change) Change {
	if c.HumanLabel == "" {
		panic(fmt.Sprintf("Change(path=%q) requires a non-empty humanLabel", c.Path))
	}
	return c
}

// NoteChange is a per-note diff inside a pattern.
type NoteChange struct {
	Kind       NoteChangeKind `json:"kind"`
	OldNote    *NoteJson      `json:"oldNote"`
	NewNote    *NoteJson      `json:"newNote"`
	HumanLabel string         `json:"humanLabel"`
}

func MakeNoteChange(c NoteChange) NoteChange {
	if c.HumanLabel == "" {
		panic("NoteChange requires a non-empty humanLabel")
	}
	return c
}

// AutomationChange is a per-keyframe diff on a controller / automation point.
type AutomationChange struct {
	Kind       ChangeKind       `json:"kind"`
	OldPoint   *AutomationPointJson `json:"oldPoint"`
	NewPoint   *AutomationPointJson `json:"newPoint"`
	HumanLabel string           `json:"humanLabel"`
}

func MakeAutomationChange(c AutomationChange) AutomationChange {
	if c.HumanLabel == "" {
		panic("AutomationChange requires a non-empty humanLabel")
	}
	return c
}

// OpaqueChange is an uninterpretable blob that changed.
type OpaqueChange struct {
	Path          string  `json:"path"`
	LocationLabel string  `json:"locationLabel"`
	OldSha256     *string `json:"oldSha256"`
	NewSha256     *string `json:"newSha256"`
	OldSize       *int    `json:"oldSize"`
	NewSize       *int    `json:"newSize"`
	HumanLabel    string  `json:"humanLabel"`
}

func MakeOpaqueChange(c OpaqueChange) OpaqueChange {
	if c.HumanLabel == "" {
		panic("OpaqueChange requires a non-empty humanLabel")
	}
	return c
}

// ChannelDiff bundles all changes affecting one channel.
type ChannelDiff struct {
	Identity          []interface{}      `json:"identity"`
	Kind              ChangeKind         `json:"kind"`
	Name              *string            `json:"name"`
	HumanLabel        string             `json:"humanLabel"`
	Changes           []Change           `json:"changes"`
	AutomationChanges []AutomationChange `json:"automationChanges"`
}

func MakeChannelDiff(d ChannelDiff) ChannelDiff {
	if d.HumanLabel == "" {
		panic("ChannelDiff requires a non-empty humanLabel")
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	if d.AutomationChanges == nil {
		d.AutomationChanges = []AutomationChange{}
	}
	return d
}

// PatternDiff bundles all changes affecting one pattern.
type PatternDiff struct {
	Identity          []interface{}      `json:"identity"`
	Kind              ChangeKind         `json:"kind"`
	Name              *string            `json:"name"`
	HumanLabel        string             `json:"humanLabel"`
	Changes           []Change           `json:"changes"`
	NoteChanges       []NoteChange       `json:"noteChanges"`
	ControllerChanges []AutomationChange `json:"controllerChanges"`
}

func MakePatternDiff(d PatternDiff) PatternDiff {
	if d.HumanLabel == "" {
		panic("PatternDiff requires a non-empty humanLabel")
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	if d.NoteChanges == nil {
		d.NoteChanges = []NoteChange{}
	}
	if d.ControllerChanges == nil {
		d.ControllerChanges = []AutomationChange{}
	}
	return d
}

// MixerInsertDiff is a per-insert mixer diff.
type MixerInsertDiff struct {
	Identity   []interface{} `json:"identity"`
	Kind       ChangeKind    `json:"kind"`
	Index      int           `json:"index"`
	Name       *string       `json:"name"`
	HumanLabel string        `json:"humanLabel"`
	Changes    []Change      `json:"changes"`
}

func MakeMixerInsertDiff(d MixerInsertDiff) MixerInsertDiff {
	if d.HumanLabel == "" {
		panic("MixerInsertDiff requires a non-empty humanLabel")
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	return d
}

// MixerDiff is the mixer branch of a DiffResult.
type MixerDiff struct {
	Inserts []MixerInsertDiff `json:"inserts"`
	Changes []Change          `json:"changes"`
}

func MakeMixerDiff(d MixerDiff) MixerDiff {
	if d.Inserts == nil {
		d.Inserts = []MixerInsertDiff{}
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	return d
}

func IsMixerDiffEmpty(m MixerDiff) bool {
	return len(m.Inserts) == 0 && len(m.Changes) == 0
}

// OldNewPos is an [oldPos, newPos] pair; marshals as a 2-element JSON array.
type OldNewPos [2]int

// ClipMoveGroup is a collapsed summary of same-ref clips shifted by the same delta.
type ClipMoveGroup struct {
	RefLabel    string      `json:"refLabel"`
	DeltaTicks  int         `json:"deltaTicks"`
	Count       int         `json:"count"`
	Positions   []OldNewPos `json:"positions"`
	ChangePaths []string    `json:"changePaths"`
	HumanLabel  string      `json:"humanLabel"`
}

func MakeClipMoveGroup(g ClipMoveGroup) ClipMoveGroup {
	if g.HumanLabel == "" {
		panic("ClipMoveGroup requires a non-empty humanLabel")
	}
	if g.Count != len(g.Positions) || g.Count != len(g.ChangePaths) {
		panic("ClipMoveGroup count must match positions/changePaths length")
	}
	if g.Count < 2 {
		panic("ClipMoveGroup requires at least 2 members")
	}
	return g
}

// ClipBulkGroup is N same-ref clips added/removed together.
type ClipBulkGroup struct {
	Kind        string   `json:"kind"` // "added" | "removed"
	RefLabel    string   `json:"refLabel"`
	LengthTicks int      `json:"lengthTicks"`
	Muted       bool     `json:"muted"`
	Count       int      `json:"count"`
	Positions   []int    `json:"positions"`
	ChangePaths []string `json:"changePaths"`
	HumanLabel  string   `json:"humanLabel"`
}

func MakeClipBulkGroup(g ClipBulkGroup) ClipBulkGroup {
	if g.HumanLabel == "" {
		panic("ClipBulkGroup requires a non-empty humanLabel")
	}
	if g.Count != len(g.Positions) || g.Count != len(g.ChangePaths) {
		panic("ClipBulkGroup count must match positions/changePaths length")
	}
	if g.Count < 2 {
		panic("ClipBulkGroup requires at least 2 members")
	}
	return g
}

// ClipModifyGroup is same-ref in-place clip modifications on one track.
type ClipModifyGroup struct {
	RefLabel       string   `json:"refLabel"`
	OldLengthTicks int      `json:"oldLengthTicks"`
	NewLengthTicks int      `json:"newLengthTicks"`
	OldMuted       bool     `json:"oldMuted"`
	NewMuted       bool     `json:"newMuted"`
	Count          int      `json:"count"`
	Positions      []int    `json:"positions"`
	ChangePaths    []string `json:"changePaths"`
	HumanLabel     string   `json:"humanLabel"`
}

func MakeClipModifyGroup(g ClipModifyGroup) ClipModifyGroup {
	if g.HumanLabel == "" {
		panic("ClipModifyGroup requires a non-empty humanLabel")
	}
	if g.Count != len(g.Positions) || g.Count != len(g.ChangePaths) {
		panic("ClipModifyGroup count must match positions/changePaths length")
	}
	if g.Count < 2 {
		panic("ClipModifyGroup requires at least 2 members")
	}
	return g
}

// TrackDiff is a per-track arrangement diff.
type TrackDiff struct {
	Identity         []interface{}     `json:"identity"`
	Kind             ChangeKind        `json:"kind"`
	Index            int               `json:"index"`
	Name             *string           `json:"name"`
	HumanLabel       string            `json:"humanLabel"`
	Changes          []Change          `json:"changes"`
	ClipMoveGroups   []ClipMoveGroup   `json:"clipMoveGroups"`
	ClipBulkGroups   []ClipBulkGroup   `json:"clipBulkGroups"`
	ClipModifyGroups []ClipModifyGroup `json:"clipModifyGroups"`
}

func MakeTrackDiff(d TrackDiff) TrackDiff {
	if d.HumanLabel == "" {
		panic("TrackDiff requires a non-empty humanLabel")
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	if d.ClipMoveGroups == nil {
		d.ClipMoveGroups = []ClipMoveGroup{}
	}
	if d.ClipBulkGroups == nil {
		d.ClipBulkGroups = []ClipBulkGroup{}
	}
	if d.ClipModifyGroups == nil {
		d.ClipModifyGroups = []ClipModifyGroup{}
	}
	return d
}

// ArrangementDiff is a per-arrangement diff.
type ArrangementDiff struct {
	Identity     []interface{} `json:"identity"`
	Kind         ChangeKind    `json:"kind"`
	Name         *string       `json:"name"`
	HumanLabel   string        `json:"humanLabel"`
	Changes      []Change      `json:"changes"`
	TrackChanges []TrackDiff   `json:"trackChanges"`
}

func MakeArrangementDiff(d ArrangementDiff) ArrangementDiff {
	if d.HumanLabel == "" {
		panic("ArrangementDiff requires a non-empty humanLabel")
	}
	if d.Changes == nil {
		d.Changes = []Change{}
	}
	if d.TrackChanges == nil {
		d.TrackChanges = []TrackDiff{}
	}
	return d
}

// DiffSummary holds aggregate counts + the pre-rendered one-line label.
type DiffSummary struct {
	TotalChanges       int    `json:"totalChanges"`
	MetadataChanges    int    `json:"metadataChanges"`
	ChannelChanges     int    `json:"channelChanges"`
	PatternChanges     int    `json:"patternChanges"`
	MixerChanges       int    `json:"mixerChanges"`
	ArrangementChanges int    `json:"arrangementChanges"`
	OpaqueChanges      int    `json:"opaqueChanges"`
	HumanLabel         string `json:"humanLabel"`
	NoteChanges        int    `json:"noteChanges"`
	AutomationChanges  int    `json:"automationChanges"`
	TrackChanges       int    `json:"trackChanges"`
}

func MakeDiffSummary(s DiffSummary) DiffSummary {
	if s.HumanLabel == "" {
		panic("DiffSummary requires a non-empty humanLabel")
	}
	return s
}

func DiffSummaryHasChanges(s DiffSummary) bool {
	return s.TotalChanges > 0 || s.NoteChanges > 0 || s.AutomationChanges > 0 || s.TrackChanges > 0
}

// DiffResult is the top-level result of diffing two projects.
type DiffResult struct {
	Summary            DiffSummary       `json:"summary"`
	MetadataChanges    []Change          `json:"metadataChanges"`
	ChannelChanges     []ChannelDiff     `json:"channelChanges"`
	PatternChanges     []PatternDiff     `json:"patternChanges"`
	MixerChanges       MixerDiff         `json:"mixerChanges"`
	ArrangementChanges []ArrangementDiff `json:"arrangementChanges"`
	OpaqueChanges      []OpaqueChange    `json:"opaqueChanges"`
}

func DiffResultIsIdentical(r DiffResult) bool { return !DiffSummaryHasChanges(r.Summary) }

// SummaryCounts is the canonical counting recipe output.
type SummaryCounts struct {
	Metadata, Channels, Patterns, Mixer, Arrangements, Opaque, Total int
}

// ComputeSummaryCounts counts entity containers with changes.
func ComputeSummaryCounts(metadata []Change, channels []ChannelDiff, patterns []PatternDiff, mixer MixerDiff, arrangements []ArrangementDiff, opaque []OpaqueChange) SummaryCounts {
	c := SummaryCounts{
		Metadata:     len(metadata),
		Channels:     len(channels),
		Patterns:     len(patterns),
		Mixer:        len(mixer.Inserts) + len(mixer.Changes),
		Arrangements: len(arrangements),
		Opaque:       len(opaque),
	}
	c.Total = c.Metadata + c.Channels + c.Patterns + c.Mixer + c.Arrangements + c.Opaque
	return c
}

// ───────────────────────── colorize ─────────────────────────

const (
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiReset  = "\x1b[0m"
)

// ColorizeSummary paints only the leading +/-/~ marker of top-level diff lines.
func ColorizeSummary(body string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = colorizeLine(l)
	}
	return strings.Join(lines, "\n")
}

func colorizeLine(line string) string {
	stripped := strings.TrimLeft(line, " ")
	indent := len(line) - len(stripped)
	if indent != 2 && indent != 6 && indent != 10 {
		return line
	}
	if len(stripped) < 2 || stripped[1] != ' ' {
		return line
	}
	var colour string
	switch stripped[0] {
	case '+':
		colour = ansiGreen
	case '-':
		colour = ansiRed
	case '~':
		colour = ansiYellow
	default:
		return line
	}
	return strings.Repeat(" ", indent) + colour + string(stripped[0]) + ansiReset + stripped[1:]
}

// ───────────────────────── headline ─────────────────────────

// Headline is version/tempo/ppq for the top-of-diff comparison.
type Headline struct {
	Version *string
	Tempo   *float64
	PPQ     int
}

// HeadlineDiff holds before/after for the headline fields.
type HeadlineDiff struct {
	VersionChanged bool
	VersionBefore  *string
	VersionAfter   *string
	TempoChanged   bool
	TempoBefore    *float64
	TempoAfter     *float64
	PPQChanged     bool
	PPQBefore      int
	PPQAfter       int
	HasChanges     bool
}

func ExtractHeadline(p *FLPProject) Headline {
	return Headline{Version: GetFLVersionBanner(p), Tempo: GetTempo(p), PPQ: p.Header.PPQ}
}

func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func floatPtrEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func DiffHeadlines(a, b Headline) HeadlineDiff {
	d := HeadlineDiff{
		VersionBefore: a.Version, VersionAfter: b.Version,
		TempoBefore: a.Tempo, TempoAfter: b.Tempo,
		PPQBefore: a.PPQ, PPQAfter: b.PPQ,
	}
	d.VersionChanged = !strPtrEq(a.Version, b.Version)
	d.TempoChanged = !floatPtrEq(a.Tempo, b.Tempo)
	d.PPQChanged = a.PPQ != b.PPQ
	d.HasChanges = d.VersionChanged || d.TempoChanged || d.PPQChanged
	return d
}

func RenderHeadlineDiff(d HeadlineDiff) string {
	if !d.HasChanges {
		return "No headline changes."
	}
	renderV := func(v *string) string {
		if v == nil {
			return "<unknown>"
		}
		return *v
	}
	renderT := func(v *float64) string {
		if v == nil {
			return "<unknown>"
		}
		return jsToFixed(*v, 1) + " BPM"
	}
	lines := []string{}
	if d.VersionChanged {
		lines = append(lines, fmt.Sprintf("~ Version: %s → %s", renderV(d.VersionBefore), renderV(d.VersionAfter)))
	}
	if d.TempoChanged {
		lines = append(lines, fmt.Sprintf("~ Tempo: %s → %s", renderT(d.TempoBefore), renderT(d.TempoAfter)))
	}
	if d.PPQChanged {
		lines = append(lines, fmt.Sprintf("~ PPQ: %d → %d", d.PPQBefore, d.PPQAfter))
	}
	return strings.Join(lines, "\n")
}
