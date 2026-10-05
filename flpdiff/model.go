package flpdiff

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

var le = binary.LittleEndian

// ───────────────────────── metadata ─────────────────────────

// FLVersion is the version recovered from opcode 0xC7. Build is nil for 3-component versions.
type FLVersion struct {
	Major int
	Minor int
	Patch int
	Build *int
}

// ProjectMetadata holds project-level decoded events. Optional fields are pointers.
type ProjectMetadata struct {
	Title                    *string
	Artists                  *string
	Genre                    *string
	Comments                 *string
	URL                      *string
	DataPath                 *string
	Version                  *FLVersion
	Looped                   *bool
	ShowInfo                 *bool
	MainPitch                *int
	CreatedOn                *time.Time
	TimeSpentSeconds         *float64
	TimeSignatureNumerator   *int
	TimeSignatureDenominator *int
	MainVolume               *int
	PanLaw                   *int
}

var delphiEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

const msPerDay = 86400000.0

// decodeTimestamp decodes the 16-byte Timestamp payload (2× float64 LE).
func decodeTimestamp(payload []byte) (createdOn time.Time, timeSpentSeconds float64, ok bool) {
	if len(payload) < 16 {
		return time.Time{}, 0, false
	}
	createdDays := math.Float64frombits(le.Uint64(payload[0:8]))
	spentDays := math.Float64frombits(le.Uint64(payload[8:16]))
	ms := pythonRound(float64(delphiEpoch.UnixMilli()) + createdDays*msPerDay)
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return time.Time{}, spentDays * 86400, true
	}
	return time.UnixMilli(int64(ms)).UTC(), spentDays * 86400, true
}

// ───────────────────────── pattern ─────────────────────────

// Note is one 24-byte note record.
type Note struct {
	Position   uint32 `json:"position"`
	Flags      int    `json:"flags"`
	Slide      bool   `json:"slide"`
	ChannelIid int    `json:"channel_iid"`
	Length     uint32 `json:"length"`
	Key        int    `json:"key"`
	Group      int    `json:"group"`
	FinePitch  int    `json:"fine_pitch"`
	Release    int    `json:"release"`
	MidiChan   int    `json:"midi_channel"`
	Pan        int    `json:"pan"`
	Velocity   int    `json:"velocity"`
	ModX       int    `json:"mod_x"`
	ModY       int    `json:"mod_y"`
}

// Controller is a 12-byte keyframe-automation controller event inside a pattern.
type Controller struct {
	Position uint32
	Channel  int
	Flags    int
	Value    float32
}

func decodeControllers(payload []byte) []Controller {
	out := []Controller{}
	if len(payload) == 0 || len(payload)%12 != 0 {
		return out
	}
	for p := 0; p+12 <= len(payload); p += 12 {
		out = append(out, Controller{
			Position: le.Uint32(payload[p:]),
			Channel:  int(payload[p+6]),
			Flags:    int(payload[p+7]),
			Value:    math.Float32frombits(le.Uint32(payload[p+8:])),
		})
	}
	return out
}

// Pattern is a pattern from the pattern rack.
type Pattern struct {
	ID          int
	Name        *string
	Length      *uint32
	Color       *RGBA
	Looped      *bool
	Notes       []Note
	Controllers []Controller
}

// NoteFlagSlide is the flags bit marking slide notes.
const NoteFlagSlide = 0x08

func decodeNotes(payload []byte) []Note {
	out := []Note{}
	if len(payload)%24 != 0 {
		return out
	}
	for p := 0; p+24 <= len(payload); p += 24 {
		flags := int(le.Uint16(payload[p+4:]))
		out = append(out, Note{
			Position:   le.Uint32(payload[p:]),
			Flags:      flags,
			Slide:      flags&NoteFlagSlide != 0,
			ChannelIid: int(le.Uint16(payload[p+6:])),
			Length:     le.Uint32(payload[p+8:]),
			Key:        int(le.Uint16(payload[p+12:])),
			Group:      int(le.Uint16(payload[p+14:])),
			FinePitch:  int(payload[p+16]),
			Release:    int(payload[p+18]),
			MidiChan:   int(payload[p+19]),
			Pan:        int(payload[p+20]),
			Velocity:   int(payload[p+21]),
			ModX:       int(payload[p+22]),
			ModY:       int(payload[p+23]),
		})
	}
	return out
}

func formatPatternSummary(patterns []Pattern) string {
	n := len(patterns)
	switch n {
	case 0:
		return "0 patterns"
	case 1:
		return "1 pattern"
	}
	return fmt.Sprintf("%d patterns", n)
}

// ───────────────────────── arrangement ─────────────────────────

// Clip is one playlist clip record.
type Clip struct {
	Position    uint32
	ItemIndex   int
	Length      uint32
	TrackRvidx  int
	Group       int
	ItemFlags   int
	StartOffset float32
	EndOffset   float32
}

func decodeClips(payload []byte, preferredRecordSize int) []Clip {
	recordSize := 0
	switch {
	case preferredRecordSize > 0 && len(payload)%preferredRecordSize == 0:
		recordSize = preferredRecordSize
	case len(payload)%80 == 0:
		recordSize = 80
	case len(payload)%60 == 0:
		recordSize = 60
	case len(payload)%32 == 0:
		recordSize = 32
	}
	if recordSize == 0 || len(payload) == 0 {
		return []Clip{}
	}
	out := []Clip{}
	for p := 0; p+recordSize <= len(payload); p += recordSize {
		out = append(out, Clip{
			Position:    le.Uint32(payload[p:]),
			ItemIndex:   int(le.Uint16(payload[p+6:])),
			Length:      le.Uint32(payload[p+8:]),
			TrackRvidx:  int(le.Uint16(payload[p+12:])),
			Group:       int(le.Uint16(payload[p+14:])),
			ItemFlags:   int(le.Uint16(payload[p+18:])),
			StartOffset: math.Float32frombits(le.Uint32(payload[p+24:])),
			EndOffset:   math.Float32frombits(le.Uint32(payload[p+28:])),
		})
	}
	return out
}

// TimeMarkerKind is "marker" or "signature".
type TimeMarkerKind string

const (
	TimeMarkerMarker    TimeMarkerKind = "marker"
	TimeMarkerSignature TimeMarkerKind = "signature"
)

// TimeMarker is a timeline marker or time-signature change.
type TimeMarker struct {
	Kind        TimeMarkerKind
	Position    uint32
	Name        *string
	Numerator   *int
	Denominator *int
}

const timeSignatureBit = 0x08000000

func decodeTimeMarkerPosition(raw uint32) (TimeMarkerKind, uint32) {
	if raw&timeSignatureBit != 0 {
		return TimeMarkerSignature, raw &^ timeSignatureBit
	}
	return TimeMarkerMarker, raw
}

// Track is a per-arrangement track descriptor (opcode 0xEE).
type Track struct {
	Index   int
	Iid     *uint32
	Name    *string
	Color   *RGBA
	Icon    *uint32
	Enabled *bool
	Height  *float32
	Locked  *bool
	Grouped *bool
}

func decodeTrackData(payload []byte, index int) Track {
	t := Track{Index: index}
	if len(payload) >= 4 {
		t.Iid = Ptr(le.Uint32(payload[0:]))
	}
	if len(payload) >= 8 {
		c := unpackRGBA(le.Uint32(payload[4:]))
		t.Color = &c
	}
	if len(payload) >= 12 {
		t.Icon = Ptr(le.Uint32(payload[8:]))
	}
	if len(payload) >= 13 {
		t.Enabled = Ptr(payload[12] != 0)
	}
	if len(payload) >= 17 {
		t.Height = Ptr(math.Float32frombits(le.Uint32(payload[13:])))
	}
	if len(payload) >= 47 {
		t.Grouped = Ptr(payload[46] != 0)
	}
	if len(payload) >= 48 {
		t.Locked = Ptr(payload[47] != 0)
	}
	return t
}

// Arrangement is a playlist arrangement.
type Arrangement struct {
	ID          int
	Name        *string
	Tracks      []Track
	Clips       []Clip
	TimeMarkers []TimeMarker
}

func formatArrangementSummary(arrs []Arrangement) string {
	n := len(arrs)
	if n == 0 {
		return "0 arrangements"
	}
	parts := make([]string, n)
	for i, a := range arrs {
		parts[i] = fmt.Sprintf("%d", len(a.Tracks))
	}
	word := "arrangements"
	if n == 1 {
		word = "arrangement"
	}
	return fmt.Sprintf("%d %s (%s tracks)", n, word, strings.Join(parts, " + "))
}

// ───────────────────────── channel ─────────────────────────

// ChannelKind classifies a channel.
type ChannelKind string

const (
	ChannelSampler    ChannelKind = "sampler"
	ChannelInstrument ChannelKind = "instrument"
	ChannelLayer      ChannelKind = "layer"
	ChannelAutomation ChannelKind = "automation"
	ChannelUnknown    ChannelKind = "unknown"
)

// ChannelPlugin is the plugin hosted on a channel.
type ChannelPlugin struct {
	InternalName string
	Name         *string
	Vendor       *string
}

// RGBA is an 8-bit-per-channel colour.
type RGBA struct {
	R, G, B, A int
}

func unpackRGBA(v uint32) RGBA {
	return RGBA{R: int(v & 0xff), G: int((v >> 8) & 0xff), B: int((v >> 16) & 0xff), A: int((v >> 24) & 0xff)}
}

// Levels is the 24-byte per-channel levels struct (opcode 0xDB).
type Levels struct {
	Pan        int32
	Volume     uint32
	PitchShift int32
	FilterModX uint32
	FilterModY uint32
	FilterType uint32
}

func filterTypeName(raw uint32) string {
	switch raw {
	case 0:
		return "FastLP"
	case 1:
		return "LP"
	case 2:
		return "BP"
	case 3:
		return "HP"
	case 4:
		return "BS"
	case 5:
		return "LPx2"
	case 6:
		return "SVFLP"
	case 7:
		return "SVFLPx2"
	}
	return "unknown"
}

func decodeLevels(payload []byte) *Levels {
	if len(payload) < 24 {
		return nil
	}
	return &Levels{
		Pan:        int32(le.Uint32(payload[0:])),
		Volume:     le.Uint32(payload[4:]),
		PitchShift: int32(le.Uint32(payload[8:])),
		FilterModX: le.Uint32(payload[12:]),
		FilterModY: le.Uint32(payload[16:]),
		FilterType: le.Uint32(payload[20:]),
	}
}

// AutomationTarget describes what an automation channel controls.
type AutomationTarget struct {
	Kind              string // "channel" | "mixer_slot" | "unknown"
	TargetChannelIid  *int
	TargetInsertIndex *int
	TargetSlotIndex   *int
	ParamID           int
	IsVstParam        bool
	RawDestination    int
}

// AutomationPoint is one keyframe on an automation curve.
type AutomationPoint struct {
	Position float64
	Value    float64
	Tension  float64
}

// Channel is a channel-rack channel.
type Channel struct {
	Iid              int
	Kind             ChannelKind
	Levels           *Levels
	Color            *RGBA
	Name             *string
	SamplePath       *string
	Plugin           *ChannelPlugin
	Enabled          *bool
	PingPongLoop     *bool
	Locked           *bool
	Zipped           *bool
	TargetInsert     *int
	AutomationPoints []AutomationPoint // nil == absent
	AutomationTarget *AutomationTarget
}

func decodeAutomationPoints(payload []byte) []AutomationPoint {
	if len(payload) < 21 {
		return []AutomationPoint{}
	}
	count := int(le.Uint32(payload[17:]))
	const recStart, recSize = 21, 24
	if count < 0 || len(payload) < recStart+count*recSize {
		return []AutomationPoint{}
	}
	out := make([]AutomationPoint, count)
	position := 0.0
	for i := 0; i < count; i++ {
		p := recStart + i*recSize
		offset := math.Float64frombits(le.Uint64(payload[p:]))
		position += offset
		value := math.Float64frombits(le.Uint64(payload[p+8:]))
		tension := float64(math.Float32frombits(le.Uint32(payload[p+16:])))
		out[i] = AutomationPoint{Position: pythonRound(position), Value: value, Tension: tension}
	}
	return out
}

func sampleFilename(path string) string {
	i := strings.LastIndex(path, "/")
	if j := strings.LastIndex(path, "\\"); j > i {
		i = j
	}
	if i < 0 {
		return path
	}
	return path[i+1:]
}

func formatSampleSummary(channels []Channel) string {
	names := []string{}
	for _, c := range channels {
		if c.SamplePath != nil {
			names = append(names, sampleFilename(*c.SamplePath))
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func classifyChannelKind(raw int) ChannelKind {
	switch raw {
	case 0:
		return ChannelSampler
	case 2, 4:
		return ChannelInstrument
	case 3:
		return ChannelLayer
	case 5:
		return ChannelAutomation
	}
	return ChannelUnknown
}

// ChannelCountsByKind counts channels per kind.
type ChannelCountsByKind map[ChannelKind]int

func countChannelsByKind(channels []Channel) ChannelCountsByKind {
	counts := ChannelCountsByKind{ChannelSampler: 0, ChannelInstrument: 0, ChannelLayer: 0, ChannelAutomation: 0, ChannelUnknown: 0}
	for _, c := range channels {
		counts[c.Kind]++
	}
	return counts
}

func formatChannelSummary(counts ChannelCountsByKind) string {
	order := []ChannelKind{ChannelAutomation, ChannelInstrument, ChannelLayer, ChannelSampler, ChannelUnknown}
	parts := []string{}
	for _, k := range order {
		n := counts[k]
		if n == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, pluralizeKind(k, n)))
	}
	if len(parts) == 0 {
		return "0 channels"
	}
	return strings.Join(parts, ", ")
}

func pluralizeKind(kind ChannelKind, n int) string {
	if n == 1 {
		return string(kind)
	}
	if kind == ChannelUnknown {
		return "unknown"
	}
	return string(kind) + "s"
}

// ───────────────────────── mixer ─────────────────────────

// MixerSlot is one effect slot on a mixer insert.
type MixerSlot struct {
	Index         int
	PluginName    *string
	HasPlugin     *bool
	InternalName  *string
	PluginVstName *string
	PluginVendor  *string
	Enabled       *bool
	Mix           *int32
}

// InsertFlags is the insert-level bitmask flags struct.
type InsertFlags struct {
	PolarityReversed          bool
	SwapLeftRight             bool
	EnableEffects             bool
	Enabled                   bool
	DisableThreadedProcessing bool
	DockMiddle                bool
	DockRight                 bool
	SeparatorShown            bool
	Locked                    bool
	Solo                      bool
	AudioTrack                bool
}

// MixerParamRecord is one record from the 0xE1 MixerParams blob.
type MixerParamRecord struct {
	ID        int
	InsertIdx int
	SlotIdx   int
	Msg       int32
}

func decodeMixerParams(payload []byte) []MixerParamRecord {
	out := []MixerParamRecord{}
	if len(payload)%12 != 0 {
		return out
	}
	for p := 0; p+12 <= len(payload); p += 12 {
		id := int(payload[p+4])
		cd := int(le.Uint16(payload[p+6:]))
		msg := int32(le.Uint32(payload[p+8:]))
		out = append(out, MixerParamRecord{ID: id, InsertIdx: (cd >> 6) & 0x7f, SlotIdx: cd & 0x3f, Msg: msg})
	}
	return out
}

func decodeInsertRouting(payload []byte) []bool {
	out := make([]bool, len(payload))
	for i, b := range payload {
		out[i] = b != 0
	}
	return out
}

func decodeInsertFlags(payload []byte) *InsertFlags {
	if len(payload) < 12 {
		return nil
	}
	raw := le.Uint32(payload[4:])
	bit := func(n uint) bool { return raw&(1<<n) != 0 }
	return &InsertFlags{
		PolarityReversed:          bit(0),
		SwapLeftRight:             bit(1),
		EnableEffects:             bit(2),
		Enabled:                   bit(3),
		DisableThreadedProcessing: bit(4),
		DockMiddle:                bit(6),
		DockRight:                 bit(7),
		SeparatorShown:            bit(10),
		Locked:                    bit(11),
		Solo:                      bit(12),
		AudioTrack:                bit(15),
	}
}

// MixerInsert is a mixer insert (FX channel).
type MixerInsert struct {
	Index            int
	Name             *string
	Color            *RGBA
	Icon             *int
	Output           *uint32
	Input            *uint32
	Flags            *InsertFlags
	Pan              *int32
	Volume           *int32
	StereoSeparation *int32
	Slots            []MixerSlot
}

func countNamedInserts(inserts []MixerInsert) int {
	n := 0
	for _, i := range inserts {
		if i.Name != nil {
			n++
		}
	}
	return n
}

func countActiveSlots(inserts []MixerInsert) int {
	n := 0
	for _, i := range inserts {
		for _, s := range i.Slots {
			if s.PluginName != nil {
				n++
			}
		}
	}
	return n
}

func formatMixerSummary(inserts []MixerInsert) string {
	parts := []string{fmt.Sprintf("%d active inserts", len(inserts))}
	if named := countNamedInserts(inserts); named > 0 {
		parts = append(parts, fmt.Sprintf("%d named", named))
	}
	if active := countActiveSlots(inserts); active > 0 {
		w := "slots"
		if active == 1 {
			w = "slot"
		}
		parts = append(parts, fmt.Sprintf("%d effect %s", active, w))
	}
	return strings.Join(parts, ", ")
}
