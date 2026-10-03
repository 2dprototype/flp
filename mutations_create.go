package flp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

const (
	opChannelTypeEvt   = 0x15
	opPluginRuntime    = 0xd4
	trackDataSize      = 70
	maxNameUTF16Length = 256
)

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// ChannelKindInput is the channel kind accepted by CreateChannel.
type ChannelKindInput string

var channelKindToByte = map[ChannelKindInput]int{"sampler": 0, "instrument": 2, "layer": 3, "automation": 5}

func findInsertionBeforeArrangements(events []FLPEvent) int {
	for i, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opArrangementNew {
			return i
		}
	}
	return len(events)
}

func nextPatternID(events []FLPEvent) int {
	max := 0
	for _, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew && int(ev.Value) > max {
			max = int(ev.Value)
		}
	}
	return max + 1
}

func nextChannelIid(events []FLPEvent) int {
	max := -1
	for _, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel && int(ev.Value) > max {
			max = int(ev.Value)
		}
	}
	return max + 1
}

// CreatePattern appends a new empty pattern; returns the project and the new id.
func CreatePattern(project *FLPProject, name string) (res *FLPProject, id int, err error) {
	defer catchMut(&err)
	if n := utf16Len(name); n > maxNameUTF16Length {
		mutErr("INVALID_ARGS", "opts.name too long (%d chars, max 256)", n)
	}
	newID := nextPatternID(project.Events)
	insertAt := findInsertionBeforeArrangements(project.Events)
	events := insertEvents(cloneEvents(project), insertAt,
		evU16(opPatternNew, newID), evBlob(opPatternName, encodeUtf16LeNullTerminated(name)))
	return withEvents(project, events), newID, nil
}

func bindUIDToFirstUnusedTrack(events []FLPEvent, uid uint32) bool {
	for i, ev := range events {
		if ev.Kind != KindBlob || ev.Opcode != opTrackData || len(ev.Payload) != trackDataSize {
			continue
		}
		if le.Uint32(ev.Payload[0:]) != 0 {
			continue
		}
		np := copyBytes(ev.Payload)
		le.PutUint32(np[0:], uid)
		events[i] = evBlob(opTrackData, np)
		return true
	}
	return false
}

func scanChannelD4UidMaxes(events []FLPEvent) (maxF9, maxF10 uint32) {
	inChannel := false
	for _, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
			inChannel = true
			continue
		}
		if ev.Opcode == opInsertFlags || ev.Opcode == opInsertEnd {
			inChannel = false
			continue
		}
		if ev.Kind == KindU16 && ev.Opcode == 0x63 {
			inChannel = false
			continue
		}
		if !inChannel {
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginRuntime && len(ev.Payload) >= 44 {
			f9 := le.Uint32(ev.Payload[36:])
			f10 := le.Uint32(ev.Payload[40:])
			if f9 > maxF9 {
				maxF9 = f9
			}
			if f10 > maxF10 {
				maxF10 = f10
			}
		}
	}
	return
}

func findFirstChannelTemplateScope(events []FLPEvent) *patternScope {
	start := -1
	for i, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	for i := start + 1; i < len(events); i++ {
		ev := events[i]
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
			return &patternScope{start, i}
		}
		if ev.Opcode == opInsertFlags || ev.Opcode == opInsertEnd {
			return &patternScope{start, i}
		}
		if ev.Kind == KindU16 && ev.Opcode == 0x63 {
			return &patternScope{start, i}
		}
	}
	return &patternScope{start, len(events)}
}

// CreateChannel appends a new channel (cloned from the first channel when one exists).
// kind defaults to "sampler" when empty.
func CreateChannel(project *FLPProject, name string, kind ChannelKindInput) (res *FLPProject, iid int, err error) {
	defer catchMut(&err)
	if kind == "" {
		kind = "sampler"
	}
	if n := utf16Len(name); n > maxNameUTF16Length {
		mutErr("INVALID_ARGS", "opts.name too long (%d chars, max 256)", n)
	}
	kindByte, ok := channelKindToByte[kind]
	if !ok {
		mutErr("INVALID_ARGS", "opts.kind must be one of: sampler, instrument, automation, layer (got %s)", kind)
	}
	newIid := nextChannelIid(project.Events)
	if newIid > 0xffff {
		mutErr("EVENT_NOT_FOUND", "cannot allocate new channel iid: max u16 (%d) reached", 0xffff)
	}
	insertAt := findInsertionBeforeArrangements(project.Events)
	template := findFirstChannelTemplateScope(project.Events)
	events := cloneEvents(project)
	if template != nil {
		maxF9, maxF10 := scanChannelD4UidMaxes(events)
		newF9, newF10 := maxF9+1, maxF10+1
		cloned := []FLPEvent{}
		for i := template.start; i < template.end; i++ {
			ev := events[i]
			if i == template.start {
				continue
			}
			if ev.Kind == KindBlob && ev.Opcode == opSamplePath {
				continue
			}
			if ev.Kind == KindU8 && ev.Opcode == opChannelTypeEvt {
				cloned = append(cloned, evU8(opChannelTypeEvt, kindByte))
				continue
			}
			if ev.Kind == KindBlob && ev.Opcode == opName {
				cloned = append(cloned, evBlob(opName, encodeUtf16LeNullTerminated(name)))
				continue
			}
			if ev.Kind == KindBlob && ev.Opcode == opPluginRuntime && len(ev.Payload) >= 44 {
				np := copyBytes(ev.Payload)
				le.PutUint32(np[36:], newF9)
				le.PutUint32(np[40:], newF10)
				cloned = append(cloned, evBlob(opPluginRuntime, np))
				continue
			}
			if ev.Kind == KindBlob {
				cloned = append(cloned, evBlob(ev.Opcode, copyBytes(ev.Payload)))
				continue
			}
			cloned = append(cloned, ev)
		}
		hasKind, hasName := false, false
		for _, ev := range cloned {
			if ev.Kind == KindU8 && ev.Opcode == opChannelTypeEvt {
				hasKind = true
			}
			if ev.Kind == KindBlob && ev.Opcode == opName {
				hasName = true
			}
		}
		newEvents := []FLPEvent{evU16(opNewChannel, newIid)}
		if !hasKind {
			newEvents = append(newEvents, evU8(opChannelTypeEvt, kindByte))
		}
		if !hasName {
			newEvents = append(newEvents, evBlob(opName, encodeUtf16LeNullTerminated(name)))
		}
		newEvents = append(newEvents, cloned...)
		events = insertEvents(events, insertAt, newEvents...)
		bindUIDToFirstUnusedTrack(events, newF9)
		return withEvents(project, events), newIid, nil
	}
	newEvents := []FLPEvent{
		evU16(opNewChannel, newIid),
		evU8(opChannelTypeEvt, kindByte),
		evBlob(opName, encodeUtf16LeNullTerminated(name)),
	}
	if kind == "instrument" {
		newEvents = insertEvents(newEvents, 2, evBlob(opPluginInternal, encodeUtf16LeNullTerminated("")))
	}
	events = insertEvents(events, insertAt, newEvents...)
	return withEvents(project, events), newIid, nil
}

// ───────────────────────── native plugin params ─────────────────────────

// PluginParamRef addresses one parameter of a native plugin.
// Kind is "main_level" | "band" | "param".
type PluginParamRef struct {
	Kind  string
	Band  float64 // band
	Field string  // band: "level" | "freq" | "width"
	Index float64 // param
}

func (p PluginParamRef) jsonString() string {
	switch p.Kind {
	case "band":
		fb, _ := json.Marshal(p.Field)
		return fmt.Sprintf(`{"kind":"band","band":%s,"field":%s}`, jsNum(p.Band), string(fb))
	case "param":
		return fmt.Sprintf(`{"kind":"param","index":%s}`, jsNum(p.Index))
	}
	return `{"kind":"main_level"}`
}

// PluginScope locates a plugin: a channel or a mixer slot.
type PluginScope struct {
	Kind        string // "channel" | "mixer_slot"
	ChannelIid  float64
	InsertIndex float64
	SlotIndex   float64
}

type fieldLoc struct {
	offset    int
	fieldType string // u8 | u16 | u32 | f32 | i32_bipolar
	scale     float64
}

type pluginLayout struct {
	minSize, maxSize int
	locate           func(ref PluginParamRef) *fieldLoc
}

func idxTable(entries map[int]fieldLoc) func(ref PluginParamRef) *fieldLoc {
	return func(ref PluginParamRef) *fieldLoc {
		if ref.Kind != "param" || !isInt(ref.Index) {
			return nil
		}
		if e, ok := entries[int(ref.Index)]; ok {
			return &e
		}
		return nil
	}
}

var eq2Layout = pluginLayout{minSize: 0x92, maxSize: 500, locate: func(ref PluginParamRef) *fieldLoc {
	if ref.Kind == "main_level" {
		return &fieldLoc{offset: 0x90, fieldType: "u16"}
	}
	if ref.Kind == "band" {
		if !isInt(ref.Band) || ref.Band < 1 || ref.Band > 7 {
			return nil
		}
		slot := (int(ref.Band) - 1) * 4
		switch ref.Field {
		case "level":
			return &fieldLoc{offset: 0x04 + slot, fieldType: "u16"}
		case "freq":
			return &fieldLoc{offset: 0x20 + slot, fieldType: "u16"}
		case "width":
			return &fieldLoc{offset: 0x3c + slot, fieldType: "u16"}
		}
	}
	return nil
}}

var reeverb2Layout = pluginLayout{minSize: 66, maxSize: 116, locate: idxTable(map[int]fieldLoc{
	0: {0x04, "u16", 0}, 1: {0x08, "u8", 0}, 2: {0x0c, "u16", 0}, 3: {0x10, "u8", 0}, 4: {0x14, "u8", 0},
	5: {0x18, "u8", 0}, 6: {0x1c, "u8", 0}, 7: {0x20, "u16", 0}, 8: {0x24, "u16", 0},
	9: {0x28, "i32_bipolar", 64}, 10: {0x2c, "u8", 0}, 11: {0x30, "u8", 0}, 12: {0x34, "u8", 0},
	13: {0x39, "u8", 0}, 14: {0x3d, "u8", 0},
})}

var limiterLayout = pluginLayout{minSize: 169, maxSize: 219, locate: idxTable(map[int]fieldLoc{
	0: {0x04, "u16", 0}, 1: {0x08, "u16", 0}, 2: {0x0c, "u16", 0}, 3: {0x10, "u16", 0}, 4: {0x14, "u8", 0},
	5: {0x18, "u16", 0}, 6: {0x1c, "u8", 0}, 7: {0x20, "u16", 0}, 8: {0x24, "u16", 0},
	9: {0x28, "i32_bipolar", 1000}, 10: {0x2c, "i32_bipolar", 1000}, 11: {0x30, "u16", 0}, 12: {0x34, "u16", 0},
	13: {0x38, "u8", 0}, 14: {0x3c, "u16", 0}, 15: {0x40, "u16", 0}, 16: {0x44, "u16", 0}, 17: {0x48, "u16", 0},
})}

// pluginLayoutOrder preserves the JS object key order for the "Known:" error text.
var pluginLayoutOrder = []string{"Fruity Parametric EQ 2", "Fruity parametric EQ 2", "Fruity Reeverb 2", "Fruity reeverb 2", "Fruity Limiter"}
var pluginParamLayouts = map[string]*pluginLayout{
	"Fruity Parametric EQ 2": &eq2Layout, "Fruity parametric EQ 2": &eq2Layout,
	"Fruity Reeverb 2": &reeverb2Layout, "Fruity reeverb 2": &reeverb2Layout,
	"Fruity Limiter": &limiterLayout,
}

// decodeUtf16LeNullTerminated strips trailing null code units then decodes.
func decodeUtf16LeNullTerminatedStrict(b []byte) string {
	end := len(b)
	for end >= 2 && b[end-1] == 0 && b[end-2] == 0 {
		end -= 2
	}
	return utf16LEToString(b[:end])
}

type pluginLoc struct {
	eventIdx   int
	pluginName string
	hasName    bool
}

func findPluginStateEvent(events []FLPEvent, scope PluginScope) *pluginLoc {
	if scope.Kind == "channel" {
		inScope := false
		var lastName string
		hasName := false
		for i, ev := range events {
			if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
				inScope = float64(ev.Value) == scope.ChannelIid
				lastName, hasName = "", false
				continue
			}
			if ev.Opcode == opInsertEnd || ev.Opcode == opInsertFlags || ev.Opcode == opNewSlot {
				return nil
			}
			if !inScope {
				continue
			}
			if ev.Kind == KindBlob && ev.Opcode == opPluginInternal {
				lastName, hasName = decodeUtf16LeNullTerminatedStrict(ev.Payload), true
			}
			if ev.Kind == KindBlob && ev.Opcode == opName {
				if n := decodeUtf16LeNullTerminatedStrict(ev.Payload); len(n) > 0 {
					lastName, hasName = n, true
				}
			}
			if ev.Kind == KindBlob && ev.Opcode == opPluginState {
				return &pluginLoc{i, lastName, hasName}
			}
		}
		return nil
	}
	insertIdx, curSlot := 0, 0
	inMixer := false
	var lastSlotName string
	hasName := false
	for i, ev := range events {
		if ev.Opcode == opInsertFlags {
			inMixer = true
			continue
		}
		if ev.Kind == KindU16 && ev.Opcode == opNewSlot {
			inMixer = true
			curSlot = int(ev.Value)
			lastSlotName, hasName = "", false
			continue
		}
		if !inMixer {
			continue
		}
		if ev.Opcode == opInsertEnd {
			insertIdx++
			curSlot = 0
			lastSlotName, hasName = "", false
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opName {
			if n := decodeUtf16LeNullTerminatedStrict(ev.Payload); len(n) > 0 {
				lastSlotName, hasName = n, true
			}
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginInternal {
			if n := decodeUtf16LeNullTerminatedStrict(ev.Payload); len(n) > 0 && !hasName {
				lastSlotName, hasName = n, true
			}
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginState {
			if float64(insertIdx) == scope.InsertIndex && float64(curSlot) == scope.SlotIndex {
				return &pluginLoc{i, lastSlotName, hasName}
			}
		}
	}
	return nil
}

// SetNativePluginParam writes a normalised (0..1) value into a native plugin's state blob.
func SetNativePluginParam(project *FLPProject, scope PluginScope, param PluginParamRef, v float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		mutErr("INVALID_ARGS", "normalizedValue must be in [0.0, 1.0], got %s", jsNum(v))
	}
	switch scope.Kind {
	case "channel":
		if !isInt(scope.ChannelIid) || scope.ChannelIid < 0 {
			mutErr("INVALID_ARGS", "scope.channelIid must be a non-negative integer, got %s", jsNum(scope.ChannelIid))
		}
	case "mixer_slot":
		if !isInt(scope.InsertIndex) || scope.InsertIndex < 0 {
			mutErr("INVALID_ARGS", "scope.insertIndex must be a non-negative integer, got %s", jsNum(scope.InsertIndex))
		}
		if !isInt(scope.SlotIndex) || scope.SlotIndex < 0 {
			mutErr("INVALID_ARGS", "scope.slotIndex must be a non-negative integer, got %s", jsNum(scope.SlotIndex))
		}
	default:
		mutErr("INVALID_ARGS", "scope.kind must be 'channel' or 'mixer_slot'")
	}
	loc := findPluginStateEvent(project.Events, scope)
	if loc == nil {
		mutErr("EVENT_NOT_FOUND", "no plugin state (0xD5) event found at the requested scope")
	}
	if !loc.hasName || loc.pluginName == "" {
		mutErr("UNSUPPORTED_PLUGIN", "plugin at the requested scope has no name event; cannot identify layout")
	}
	layout := pluginParamLayouts[loc.pluginName]
	if layout == nil {
		mutErr("UNSUPPORTED_PLUGIN", "plugin \"%s\" has no registered param layout. Known: %s", loc.pluginName, strings.Join(pluginLayoutOrder, ", "))
	}
	ev := project.Events[loc.eventIdx]
	if ev.Kind != KindBlob {
		mutErr("EVENT_NOT_FOUND", "0xD5 event is not a blob")
	}
	if len(ev.Payload) < layout.minSize || len(ev.Payload) > layout.maxSize {
		mutErr("UNSUPPORTED_PLUGIN", "plugin \"%s\" state blob is %d bytes, expected [%d, %d]; layout may be out of date",
			loc.pluginName, len(ev.Payload), layout.minSize, layout.maxSize)
	}
	info := layout.locate(param)
	if info == nil {
		mutErr("INVALID_ARGS", "param ref %s not recognised for plugin \"%s\"", param.jsonString(), loc.pluginName)
	}
	np := copyBytes(ev.Payload)
	off := info.offset
	switch info.fieldType {
	case "u16":
		raw := int64(jsRound(v * 0xffff))
		np[off] = byte(raw & 0xff)
		np[off+1] = byte((raw >> 8) & 0xff)
	case "u8":
		raw := int64(jsRound(v * 0xff))
		np[off] = byte(raw & 0xff)
	case "u32":
		le.PutUint32(np[off:], uint32(jsRound(v*0xffffffff)))
	case "f32":
		le.PutUint32(np[off:], math.Float32bits(float32(v)))
	case "i32_bipolar":
		raw := int32(jsRound((v*2 - 1) * info.scale))
		le.PutUint32(np[off:], uint32(raw))
	}
	events := cloneEvents(project)
	events[loc.eventIdx] = evBlob(opPluginState, np)
	return withEvents(project, events), nil
}

// ───────────────────────── pattern length ─────────────────────────

// SetPatternLength sets (or inserts) a pattern's length in ticks.
func SetPatternLength(project *FLPProject, patternID int, ticks int64) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	if ticks < 0 || ticks > 0xffffffff {
		mutErr("INVALID_ARGS", "pattern length must be a non-negative u32, got %d", ticks)
	}
	sc := mustScope(project.Events, patternID)
	events := cloneEvents(project)
	for i := sc.start + 1; i < sc.end; i++ {
		if events[i].Opcode == opPatternLength {
			events[i] = evU32(opPatternLength, uint32(ticks))
			return withEvents(project, events), nil
		}
	}
	insertAt := sc.start + 1
	for i := sc.start + 1; i < sc.end; i++ {
		if events[i].Opcode == opPatternName {
			insertAt = i + 1
			break
		}
	}
	events = insertEvents(events, insertAt, evU32(opPatternLength, uint32(ticks)))
	return withEvents(project, events), nil
}

func findPatternLength(events []FLPEvent, sc *patternScope) int64 {
	for i := sc.start + 1; i < sc.end; i++ {
		if events[i].Opcode == opPatternLength && events[i].Kind == KindU32 {
			return int64(events[i].Value)
		}
	}
	return 0
}

func notesEndTick(notes []NoteInput) float64 {
	end := 0.0
	for _, n := range notes {
		if e := n.Position + n.Length; e > end {
			end = e
		}
	}
	return end
}

func ceilToBeat(ticks float64, ppq int) float64 {
	if ppq <= 0 {
		return ticks
	}
	return math.Ceil(ticks/float64(ppq)) * float64(ppq)
}

func setPatternNotesAutoGrow(project *FLPProject, patternID int, notes []NoteInput, grow bool) *FLPProject {
	next := must(SetPatternNotes(project, patternID, notes))
	if !grow {
		return next
	}
	sc := findPatternScope(next.Events, patternID)
	cur := findPatternLength(next.Events, sc)
	required := notesEndTick(notes)
	if cur > 0 && required > float64(cur) {
		next = must(SetPatternLength(next, patternID, int64(ceilToBeat(required, next.Header.PPQ))))
	}
	return next
}

// mulberry32 is a small deterministic PRNG.
func mulberry32(seed float64) func() float64 {
	t := uint32(toInt32(seed))
	return func() float64 {
		t += 0x6d2b79f5
		r := t
		r = (r ^ (r >> 15)) * (r | 1)
		r ^= r + (r^(r>>7))*(r|61)
		return float64(r^(r>>14)) / 4294967296
	}
}

func clampInt(value, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, jsRound(value)))
}

