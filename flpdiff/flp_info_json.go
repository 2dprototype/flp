package flpdiff

import (
	"fmt"
	"math"
)

// ───────────── JSON shape types (mirroring Python flp-info output) ─────────────

type RgbaJson struct {
	Type  string  `json:"_type"`
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

type PathJson struct {
	Type  string `json:"_type"`
	Value string `json:"value"`
}

type PluginJson struct {
	Type   string      `json:"_type"`
	Name   string      `json:"name"`
	Vendor *string     `json:"vendor"`
	IsVst  bool        `json:"is_vst"`
	State  interface{} `json:"state"`
}

type AutomationPointJson struct {
	Type     string  `json:"_type"`
	Position float64 `json:"position"`
	Value    float64 `json:"value"`
	Tension  float64 `json:"tension"`
}

type ChannelJson struct {
	Type             string                `json:"_type"`
	Iid              int                   `json:"iid"`
	Kind             string                `json:"kind"`
	Name             *string               `json:"name"`
	SamplePath       *PathJson             `json:"sample_path"`
	Plugin           *PluginJson           `json:"plugin"`
	Color            *RgbaJson             `json:"color"`
	Pan              float64               `json:"pan"`
	Volume           float64               `json:"volume"`
	Enabled          bool                  `json:"enabled"`
	Muted            bool                  `json:"muted"`
	TargetInsert     *int                  `json:"target_insert"`
	AutomationPoints []AutomationPointJson `json:"automation_points"`
}

type NoteJson struct {
	Type       string `json:"_type"`
	Position   uint32 `json:"position"`
	Length     uint32 `json:"length"`
	Key        int    `json:"key"`
	ChannelIid int    `json:"channel_iid"`
	Pan        int    `json:"pan"`
	Velocity   int    `json:"velocity"`
	FinePitch  int    `json:"fine_pitch"`
	Release    int    `json:"release"`
}

type PatternJson struct {
	Type        string                `json:"_type"`
	Iid         int                   `json:"iid"`
	Name        *string               `json:"name"`
	Color       *RgbaJson             `json:"color"`
	Length      *uint32               `json:"length"`
	Looped      bool                  `json:"looped"`
	Notes       []NoteJson            `json:"notes"`
	Controllers []AutomationPointJson `json:"controllers"`
}

type MixerSlotJson struct {
	Type    string      `json:"_type"`
	Index   int         `json:"index"`
	Enabled bool        `json:"enabled"`
	Plugin  *PluginJson `json:"plugin"`
}

type MixerInsertJson struct {
	Type             string          `json:"_type"`
	Index            int             `json:"index"`
	Name             *string         `json:"name"`
	Color            *RgbaJson       `json:"color"`
	Enabled          bool            `json:"enabled"`
	Locked           bool            `json:"locked"`
	Pan              *float64        `json:"pan"`
	Volume           *float64        `json:"volume"`
	StereoSeparation *float64        `json:"stereo_separation"`
	Slots            []MixerSlotJson `json:"slots"`
	RoutesTo         []int           `json:"routes_to"`
}

type MixerJson struct {
	Type    string            `json:"_type"`
	Inserts []MixerInsertJson `json:"inserts"`
}

type PlaylistItemJson struct {
	Type       string `json:"_type"`
	Position   uint32 `json:"position"`
	Length     uint32 `json:"length"`
	PatternIid *int   `json:"pattern_iid"`
	ChannelIid *int   `json:"channel_iid"`
	Muted      bool   `json:"muted"`
}

type TrackJson struct {
	Type   string             `json:"_type"`
	Index  int                `json:"index"`
	Name   *string            `json:"name"`
	Color  *RgbaJson          `json:"color"`
	Height float64            `json:"height"`
	Muted  bool               `json:"muted"`
	Items  []PlaylistItemJson `json:"items"`
}

type TimeMarkerJson struct {
	Type        string  `json:"_type"`
	Position    uint32  `json:"position"`
	Name        *string `json:"name"`
	Numerator   *int    `json:"numerator"`
	Denominator *int    `json:"denominator"`
}

type ArrangementJson struct {
	Type        string           `json:"_type"`
	Index       int              `json:"index"`
	Name        *string          `json:"name"`
	Tracks      []TrackJson      `json:"tracks"`
	TimeMarkers []TimeMarkerJson `json:"timemarkers"`
}

type FLVersionJson struct {
	Type  string `json:"_type"`
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Patch int    `json:"patch"`
	Build *int   `json:"build,omitempty"`
}

type TimeSignatureJson struct {
	Type        string `json:"_type"`
	Numerator   int    `json:"numerator"`
	Denominator int    `json:"denominator"`
}

type DatetimeJson struct {
	Type string `json:"_type"`
	Iso  string `json:"iso"`
}

type TimedeltaJson struct {
	Type    string  `json:"_type"`
	Seconds float64 `json:"seconds"`
}

type MetadataJson struct {
	Type          string             `json:"_type"`
	Title         string             `json:"title"`
	Artists       string             `json:"artists"`
	Genre         string             `json:"genre"`
	Comments      string             `json:"comments"`
	Format        string             `json:"format"`
	PPQ           int                `json:"ppq"`
	Tempo         float64            `json:"tempo"`
	TimeSignature *TimeSignatureJson `json:"time_signature"`
	MainPitch     int                `json:"main_pitch"`
	MainVolume    *int               `json:"main_volume"`
	PanLaw        int                `json:"pan_law"`
	Looped        bool               `json:"looped"`
	ShowInfo      bool               `json:"show_info"`
	URL           *string            `json:"url"`
	DataPath      *PathJson          `json:"data_path"`
	CreatedOn     *DatetimeJson      `json:"created_on"`
	TimeSpent     *TimedeltaJson     `json:"time_spent"`
	Version       FLVersionJson      `json:"version"`
}

type OpaqueEventJson struct {
	Type    string  `json:"_type"`
	EventID int     `json:"event_id"`
	Sha256  string  `json:"sha256"`
	Size    int     `json:"size"`
	Hint    *string `json:"hint"`
}

// FlpInfoJson is the full project projection.
type FlpInfoJson struct {
	Type         string            `json:"_type"`
	Metadata     MetadataJson      `json:"metadata"`
	Channels     []ChannelJson     `json:"channels"`
	Patterns     []PatternJson     `json:"patterns"`
	Mixer        MixerJson         `json:"mixer"`
	Arrangements []ArrangementJson `json:"arrangements"`
	OpaqueEvents []OpaqueEventJson `json:"opaque_events"`
	ScoreLog     []interface{}     `json:"score_log"`
}

// ───────────── helpers ─────────────

func rgbaToJson(c *RGBA) *RgbaJson {
	if c == nil {
		return nil
	}
	return &RgbaJson{Type: "RGBA", Red: float64(c.R) / 255, Green: float64(c.G) / 255, Blue: float64(c.B) / 255, Alpha: float64(c.A) / 255}
}

func pathToJson(v *string) *PathJson {
	if v == nil {
		return nil
	}
	return &PathJson{Type: "path", Value: *v}
}

func normalisePath(raw string) string {
	if len(raw) <= 1 {
		return raw
	}
	if raw[len(raw)-1] == '/' {
		return raw[:len(raw)-1]
	}
	return raw
}

var defaultChannelColor = RGBA{R: 65, G: 69, B: 72, A: 0}

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func pluginToJson(p *ChannelPlugin) *PluginJson {
	if p == nil {
		return nil
	}
	isVst := p.InternalName == "Fruity Wrapper"
	name := p.InternalName
	if isVst && p.Name != nil {
		name = *p.Name
	}
	var vendor *string
	if isVst && p.Vendor != nil {
		vendor = p.Vendor
	}
	return &PluginJson{Type: "Plugin", Name: name, Vendor: vendor, IsVst: isVst, State: nil}
}

func slotPluginToJson(s MixerSlot) *PluginJson {
	if s.HasPlugin == nil || !*s.HasPlugin {
		return nil
	}
	isVst := s.InternalName != nil && *s.InternalName == "Fruity Wrapper"
	name := ""
	if s.PluginName != nil {
		name = *s.PluginName
	}
	if name == "" && isVst && s.PluginVstName != nil {
		name = *s.PluginVstName
	}
	if name == "" && s.InternalName != nil {
		name = *s.InternalName
	}
	var vendor *string
	if isVst {
		vendor = s.PluginVendor
	}
	return &PluginJson{Type: "Plugin", Name: name, Vendor: vendor, IsVst: isVst, State: nil}
}

func toChannelJson(ch Channel) ChannelJson {
	rawPan := 6400.0
	rawVol := 10000.0
	if ch.Levels != nil {
		rawPan = float64(ch.Levels.Pan)
		rawVol = float64(ch.Levels.Volume)
	}
	pan := clampF(rawPan/6400, -1, 1)
	volume := rawVol / 12800
	hasMixRouting := ch.Kind == ChannelSampler || ch.Kind == ChannelInstrument
	hasSamplePath := ch.Kind == ChannelSampler
	color := defaultChannelColor
	if ch.Color != nil {
		color = *ch.Color
	}
	enabled := true
	if ch.Enabled != nil {
		enabled = *ch.Enabled
	}
	var samplePath *PathJson
	if hasSamplePath {
		samplePath = pathToJson(ch.SamplePath)
	}
	var targetInsert *int
	if hasMixRouting {
		targetInsert = ch.TargetInsert
	}
	points := make([]AutomationPointJson, 0, len(ch.AutomationPoints))
	for _, p := range ch.AutomationPoints {
		points = append(points, AutomationPointJson{Type: "AutomationPoint", Position: p.Position, Value: p.Value, Tension: p.Tension})
	}
	return ChannelJson{
		Type: "Channel", Iid: ch.Iid, Kind: string(ch.Kind), Name: ch.Name,
		SamplePath: samplePath, Plugin: pluginToJson(ch.Plugin), Color: rgbaToJson(&color),
		Pan: pan, Volume: volume, Enabled: enabled, Muted: false,
		TargetInsert: targetInsert, AutomationPoints: points,
	}
}

func toNoteJson(n Note) NoteJson {
	return NoteJson{Type: "Note", Position: n.Position, Length: n.Length, Key: n.Key, ChannelIid: n.ChannelIid,
		Pan: n.Pan, Velocity: n.Velocity, FinePitch: n.FinePitch, Release: n.Release}
}

func toPatternJson(p Pattern) PatternJson {
	notes := make([]NoteJson, 0, len(p.Notes))
	for _, n := range p.Notes {
		notes = append(notes, toNoteJson(n))
	}
	ctrls := make([]AutomationPointJson, 0, len(p.Controllers))
	for _, c := range p.Controllers {
		ctrls = append(ctrls, AutomationPointJson{Type: "AutomationPoint", Position: float64(c.Position), Value: float64(c.Value), Tension: 0})
	}
	looped := false
	if p.Looped != nil {
		looped = *p.Looped
	}
	return PatternJson{Type: "Pattern", Iid: p.ID, Name: p.Name, Color: rgbaToJson(p.Color), Length: p.Length,
		Looped: looped, Notes: notes, Controllers: ctrls}
}

func toMixerSlotJson(s MixerSlot) MixerSlotJson {
	return MixerSlotJson{Type: "MixerSlot", Index: s.Index, Enabled: true, Plugin: slotPluginToJson(s)}
}

func toMixerInsertJson(ins MixerInsert) MixerInsertJson {
	enabled, locked := false, false
	if ins.Flags != nil {
		enabled = ins.Flags.Enabled
		locked = ins.Flags.Locked
	}
	var pan, vol, sep *float64
	if ins.Pan != nil {
		pan = Ptr(clampF(float64(*ins.Pan)/6400, -1, 1))
	}
	if ins.Volume != nil {
		vol = Ptr(float64(*ins.Volume) / 12800)
	}
	if ins.StereoSeparation != nil {
		sep = Ptr(clampF(float64(*ins.StereoSeparation)/6400, -1, 1))
	}
	slots := make([]MixerSlotJson, 0, len(ins.Slots))
	for _, s := range ins.Slots {
		slots = append(slots, toMixerSlotJson(s))
	}
	return MixerInsertJson{Type: "MixerInsert", Index: ins.Index, Name: ins.Name, Color: rgbaToJson(ins.Color),
		Enabled: enabled, Locked: locked, Pan: pan, Volume: vol, StereoSeparation: sep, Slots: slots, RoutesTo: []int{}}
}

func toTimeMarkerJson(m TimeMarker) TimeMarkerJson {
	return TimeMarkerJson{Type: "TimeMarker", Position: m.Position, Name: m.Name, Numerator: m.Numerator, Denominator: m.Denominator}
}

func toPlaylistItemJson(c Clip) PlaylistItemJson {
	isPattern := c.ItemIndex > playlistPatternBase
	it := PlaylistItemJson{Type: "PlaylistItem", Position: c.Position, Length: c.Length, Muted: false}
	if isPattern {
		it.PatternIid = Ptr(c.ItemIndex - playlistPatternBase)
	} else {
		it.ChannelIid = Ptr(c.ItemIndex)
	}
	return it
}

const trackRvidxMax = 499

func toTrackJson(t Track, items []PlaylistItemJson) TrackJson {
	index := t.Index + 1
	if t.Iid != nil && *t.Iid != 0 {
		index = int(*t.Iid)
	}
	height := 1.0
	if t.Height != nil {
		height = math.Trunc(float64(*t.Height)*100) / 100
	}
	enabled := true
	if t.Enabled != nil {
		enabled = *t.Enabled
	}
	if items == nil {
		items = []PlaylistItemJson{}
	}
	return TrackJson{Type: "Track", Index: index, Name: t.Name, Color: rgbaToJson(t.Color), Height: height, Muted: !enabled, Items: items}
}

func toArrangementJson(a Arrangement) ArrangementJson {
	byTrack := map[int][]PlaylistItemJson{}
	for _, clip := range a.Clips {
		idx := trackRvidxMax - clip.TrackRvidx
		byTrack[idx] = append(byTrack[idx], toPlaylistItemJson(clip))
	}
	tracks := make([]TrackJson, 0, len(a.Tracks))
	for _, t := range a.Tracks {
		tracks = append(tracks, toTrackJson(t, byTrack[t.Index]))
	}
	markers := make([]TimeMarkerJson, 0, len(a.TimeMarkers))
	for _, m := range a.TimeMarkers {
		markers = append(markers, toTimeMarkerJson(m))
	}
	return ArrangementJson{Type: "Arrangement", Index: a.ID, Name: a.Name, Tracks: tracks, TimeMarkers: markers}
}

func strOr(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

func datetimeToJson(t *timeValue) *DatetimeJson {
	if t == nil {
		return nil
	}
	return &DatetimeJson{Type: "datetime", Iso: fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d.%03d000",
		t.Year, t.Month, t.Day, t.Hour, t.Min, t.Sec, t.Milli)}
}

func getTempoFromProject(p *FLPProject) float64 {
	if t := GetTempo(p); t != nil {
		return *t
	}
	return 120.0
}

func toMetadataJson(p *FLPProject, tempo float64) MetadataJson {
	m := p.Metadata
	var ts *TimeSignatureJson
	if m.TimeSignatureNumerator != nil && m.TimeSignatureDenominator != nil {
		ts = &TimeSignatureJson{Type: "TimeSignature", Numerator: *m.TimeSignatureNumerator, Denominator: *m.TimeSignatureDenominator}
	}
	mainPitch := 0
	if m.MainPitch != nil {
		mainPitch = *m.MainPitch
	}
	looped, showInfo := false, false
	if m.Looped != nil {
		looped = *m.Looped
	}
	if m.ShowInfo != nil {
		showInfo = *m.ShowInfo
	}
	dataPath := "."
	if m.DataPath != nil {
		dataPath = *m.DataPath
	}
	version := FLVersionJson{Type: "FLVersion"}
	if m.Version != nil {
		version.Major, version.Minor, version.Patch, version.Build = m.Version.Major, m.Version.Minor, m.Version.Patch, m.Version.Build
	}
	var created *DatetimeJson
	if m.CreatedOn != nil {
		tv := toTimeValue(*m.CreatedOn)
		created = datetimeToJson(&tv)
	}
	var spent *TimedeltaJson
	if m.TimeSpentSeconds != nil {
		spent = &TimedeltaJson{Type: "timedelta", Seconds: *m.TimeSpentSeconds}
	}
	return MetadataJson{
		Type: "ProjectMetadata", Title: strOr(m.Title, ""), Artists: strOr(m.Artists, ""), Genre: strOr(m.Genre, ""),
		Comments: strOr(m.Comments, ""), Format: "project", PPQ: p.Header.PPQ, Tempo: tempo, TimeSignature: ts,
		MainPitch: mainPitch, MainVolume: nil, PanLaw: 0, Looped: looped, ShowInfo: showInfo, URL: m.URL,
		DataPath: &PathJson{Type: "path", Value: normalisePath(dataPath)},
		CreatedOn: created, TimeSpent: spent, Version: version,
	}
}

// ToFlpInfoJson projects a raw FLPProject into Python flp-info's JSON shape.
func ToFlpInfoJson(p *FLPProject) FlpInfoJson {
	tempo := getTempoFromProject(p)
	channels := make([]ChannelJson, 0, len(p.Channels))
	for _, c := range p.Channels {
		channels = append(channels, toChannelJson(c))
	}
	patterns := make([]PatternJson, 0, len(p.Patterns))
	for _, pt := range p.Patterns {
		patterns = append(patterns, toPatternJson(pt))
	}
	inserts := make([]MixerInsertJson, 0, len(p.Inserts))
	for _, ins := range p.Inserts {
		inserts = append(inserts, toMixerInsertJson(ins))
	}
	arrs := make([]ArrangementJson, 0, len(p.Arrangements))
	for _, a := range p.Arrangements {
		arrs = append(arrs, toArrangementJson(a))
	}
	return FlpInfoJson{
		Type: "FLPProject", Metadata: toMetadataJson(p, tempo), Channels: channels, Patterns: patterns,
		Mixer: MixerJson{Type: "Mixer", Inserts: inserts}, Arrangements: arrs,
		OpaqueEvents: []OpaqueEventJson{}, ScoreLog: []interface{}{},
	}
}
