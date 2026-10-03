package flp

import (
	"fmt"
	"math"
	"time"
)

func existingNoteInputs(project *FLPProject, patternID int) []NoteInput {
	sc := mustScope(project.Events, patternID)
	notes := collectExistingNotes(project.Events, sc)
	out := make([]NoteInput, len(notes))
	for i, n := range notes {
		out[i] = NoteToInput(n)
	}
	return out
}

// TransposePatternNotes shifts keys by semitones (optionally only one channel).
func TransposePatternNotes(project *FLPProject, patternID, semitones int, channelIid *int) (res *FLPProject, err error) {
	defer catchMut(&err)
	next := existingNoteInputs(project, patternID)
	for i, n := range next {
		if channelIid != nil && n.ChannelIid != float64(*channelIid) {
			continue
		}
		next[i].Key = clampInt(n.Key+float64(semitones), 0, 131)
	}
	return setPatternNotesAutoGrow(project, patternID, next, false), nil
}

// QuantizePatternNotes snaps note starts toward a grid by strength (0..1).
func QuantizePatternNotes(project *FLPProject, patternID, gridTicks int, strength float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if gridTicks <= 0 {
		mutErr("INVALID_ARGS", "gridTicks must be a positive integer, got %d", gridTicks)
	}
	if math.IsNaN(strength) || math.IsInf(strength, 0) || strength < 0 || strength > 1 {
		mutErr("INVALID_ARGS", "strength must be in [0, 1], got %s", jsNum(strength))
	}
	next := existingNoteInputs(project, patternID)
	g := float64(gridTicks)
	for i, n := range next {
		target := jsRound(n.Position/g) * g
		newPos := jsRound(n.Position + (target-n.Position)*strength)
		next[i].Position = math.Max(0, newPos)
	}
	return setPatternNotesAutoGrow(project, patternID, next, true), nil
}

func seedOrNow(seed *float64) float64 {
	if seed != nil {
		return *seed
	}
	return float64(time.Now().UnixMilli())
}

// HumanizeVelocities adds random velocity jitter within ±rangeV.
func HumanizeVelocities(project *FLPProject, patternID, rangeV int, seed *float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if rangeV < 0 {
		mutErr("INVALID_ARGS", "range must be a non-negative integer, got %d", rangeV)
	}
	existing := existingNoteInputs(project, patternID)
	if rangeV == 0 {
		return setPatternNotesAutoGrow(project, patternID, existing, false), nil
	}
	rnd := mulberry32(seedOrNow(seed))
	for i, n := range existing {
		jitter := jsRound((rnd()*2 - 1) * float64(rangeV))
		existing[i].Velocity = clampInt(n.Velocity+jitter, 1, 127)
	}
	return setPatternNotesAutoGrow(project, patternID, existing, false), nil
}

// HumanizeTimings adds random position jitter within ±rangeTicks.
func HumanizeTimings(project *FLPProject, patternID, rangeTicks int, seed *float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if rangeTicks < 0 {
		mutErr("INVALID_ARGS", "rangeTicks must be a non-negative integer, got %d", rangeTicks)
	}
	existing := existingNoteInputs(project, patternID)
	if rangeTicks == 0 {
		return setPatternNotesAutoGrow(project, patternID, existing, true), nil
	}
	rnd := mulberry32(seedOrNow(seed))
	for i, n := range existing {
		jitter := jsRound((rnd()*2 - 1) * float64(rangeTicks))
		existing[i].Position = math.Max(0, n.Position+jitter)
	}
	return setPatternNotesAutoGrow(project, patternID, existing, true), nil
}

// ReversePatternNotes mirrors notes in time around the pattern length.
func ReversePatternNotes(project *FLPProject, patternID int) (res *FLPProject, err error) {
	defer catchMut(&err)
	sc := mustScope(project.Events, patternID)
	existing := existingNoteInputs(project, patternID)
	if len(existing) == 0 {
		return project, nil
	}
	declared := findPatternLength(project.Events, sc)
	axis := notesEndTick(existing)
	if declared > 0 {
		axis = float64(declared)
	}
	for i, n := range existing {
		existing[i].Position = math.Max(0, axis-n.Position-n.Length)
	}
	return setPatternNotesAutoGrow(project, patternID, existing, false), nil
}

// InvertPatternNotes mirrors note keys around axisKey.
func InvertPatternNotes(project *FLPProject, patternID, axisKey int) (res *FLPProject, err error) {
	defer catchMut(&err)
	if axisKey < 0 || axisKey > 131 {
		mutErr("INVALID_ARGS", "axisKey must be in [0, 131], got %d", axisKey)
	}
	existing := existingNoteInputs(project, patternID)
	for i, n := range existing {
		existing[i].Key = clampInt(float64(2*axisKey)-n.Key, 0, 131)
	}
	return setPatternNotesAutoGrow(project, patternID, existing, false), nil
}

// ───────────────────────── channel volume / pan ─────────────────────────

const (
	channelVolumeMax = 12800
	channelPanMax    = 6400
)

func findChannelLevelsEvent(events []FLPEvent, iid int) int {
	inScope := false
	for i, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
			inScope = int(ev.Value) == iid
			continue
		}
		if !inScope {
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opChannelLevels {
			return i
		}
	}
	return -1
}

func defaultChannelLevelsPayload() []byte {
	buf := make([]byte, 24)
	le.PutUint32(buf[0:], uint32(int32(6400)))
	le.PutUint32(buf[4:], 10000)
	le.PutUint32(buf[8:], 0)
	le.PutUint32(buf[12:], 256)
	le.PutUint32(buf[16:], 0)
	le.PutUint32(buf[20:], 0)
	return buf
}

func patchChannelLevels(project *FLPProject, iid int, patch func(p []byte)) *FLPProject {
	if iid < 0 {
		mutErr("INVALID_ARGS", "channel iid must be a non-negative integer, got %d", iid)
	}
	events := cloneEvents(project)
	idx := findChannelLevelsEvent(events, iid)
	if idx < 0 {
		open := findOpen(events, KindU16, opNewChannel, iid)
		if open == -1 {
			mutErr("EVENT_NOT_FOUND", "no channel with iid=%d found", iid)
		}
		events = insertEvents(events, open+1, evBlob(opChannelLevels, defaultChannelLevelsPayload()))
		idx = open + 1
	}
	ev := events[idx]
	if ev.Kind != KindBlob || len(ev.Payload) < 24 {
		mutErr("EVENT_NOT_FOUND", "0xDB event for channel %d is not a 24+ byte blob", iid)
	}
	np := copyBytes(ev.Payload)
	patch(np)
	events[idx] = evBlob(opChannelLevels, np)
	return withEvents(project, events)
}

// SetChannelVolume sets channel volume (0..1 normalised).
func SetChannelVolume(project *FLPProject, iid int, normalized float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if math.IsNaN(normalized) || math.IsInf(normalized, 0) || normalized < 0 || normalized > 1 {
		mutErr("INVALID_ARGS", "volume must be in [0, 1], got %s", jsNum(normalized))
	}
	return patchChannelLevels(project, iid, func(p []byte) {
		le.PutUint32(p[4:], uint32(jsRound(normalized*channelVolumeMax)))
	}), nil
}

// SetChannelPan sets channel pan (-1..+1 normalised).
func SetChannelPan(project *FLPProject, iid int, normalized float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if math.IsNaN(normalized) || math.IsInf(normalized, 0) || normalized < -1 || normalized > 1 {
		mutErr("INVALID_ARGS", "pan must be in [-1, +1], got %s", jsNum(normalized))
	}
	return patchChannelLevels(project, iid, func(p []byte) {
		le.PutUint32(p[0:], uint32(int32(jsRound(normalized*channelPanMax))))
	}), nil
}

// ───────────────────────── arrange_song ─────────────────────────

// SongSection is one section of an arranged song.
type SongSection struct {
	PatternID     int
	Bars          int
	PositionTicks *float64
}

// ArrangeSongOptions configures ArrangeSong. Nil fields take defaults (track 0, 4 beats/bar).
type ArrangeSongOptions struct {
	TrackIndex  *float64
	BeatsPerBar *float64
}

// ArrangeSong lays pattern sections end-to-end on a track.
func ArrangeSong(project *FLPProject, arrangementID int, structure []SongSection, opts ArrangeSongOptions) (res *FLPProject, err error) {
	defer catchMut(&err)
	if len(structure) == 0 {
		mutErr("INVALID_ARGS", "structure must be a non-empty array of sections")
	}
	trackIndex := 0.0
	if opts.TrackIndex != nil {
		trackIndex = *opts.TrackIndex
	}
	beatsPerBar := 4.0
	if opts.BeatsPerBar != nil {
		beatsPerBar = *opts.BeatsPerBar
	}
	if !isInt(beatsPerBar) || beatsPerBar < 1 {
		mutErr("INVALID_ARGS", "beats_per_bar must be positive integer, got %s", jsNum(beatsPerBar))
	}
	ppq := float64(project.Header.PPQ)
	cursor := 0.0
	next := project
	for i, s := range structure {
		if s.PatternID < 1 {
			mutErr("INVALID_ARGS", "structure[%d].pattern_id must be positive integer, got %d", i, s.PatternID)
		}
		if s.Bars < 1 {
			mutErr("INVALID_ARGS", "structure[%d].bars must be positive integer, got %d", i, s.Bars)
		}
		lengthTicks := float64(s.Bars) * beatsPerBar * ppq
		positionTicks := cursor
		if s.PositionTicks != nil {
			positionTicks = *s.PositionTicks
		}
		if !isInt(positionTicks) || positionTicks < 0 {
			mutErr("INVALID_ARGS", "structure[%d].position_ticks must be non-negative integer, got %s", i, jsNum(positionTicks))
		}
		next = must(AddClip(next, arrangementID, ClipPlacement{
			Kind: "pattern", RefID: float64(s.PatternID), TrackIndex: trackIndex,
			PositionTicks: positionTicks, LengthTicks: lengthTicks,
		}))
		cursor = positionTicks + lengthTicks
	}
	return next, nil
}

// ───────────────────────── plugin instantiation ─────────────────────────

// InstantiateNativePlugin splices a plugin scope from a donor FLP into a mixer slot.
// Returns the new project and the FL IPC slot index (slot_marker + 1).
func InstantiateNativePlugin(project, donor *FLPProject, pluginName string, insertIndex, slotMarker int) (res *FLPProject, flIPCSlot int, err error) {
	defer catchMut(&err)
	extracted, e := ExtractPluginSlotScope(donor, pluginName)
	if e != nil {
		mutErr("PLUGIN_INSTANTIATE_FAILED", "donor extraction for \"%s\": %s", pluginName, e.Error())
	}
	mutated, e := CraftPluginFixture(project, extracted.Scope, insertIndex, slotMarker)
	if e != nil {
		mutErr("PLUGIN_INSTANTIATE_FAILED", "splice into insert=%d slot_marker=%d: %s", insertIndex, slotMarker, e.Error())
	}
	return mutated, slotMarker + 1, nil
}

// LoadFactoryGeneratorPreset adds a new channel built from a factory .fst generator preset.
func LoadFactoryGeneratorPreset(project, donor *FLPProject, name *string) (res *FLPProject, iid int, err error) {
	defer catchMut(&err)
	extracted, e := ExtractPluginScopeFromFst(donor)
	if e != nil {
		if fe, ok := e.(*FstParseError); ok {
			mutErr("PLUGIN_INSTANTIATE_FAILED", "donor .fst: %s", fe.Message)
		}
		panic(e)
	}
	displayName := extracted.PluginInternalName
	if extracted.PluginDisplayName != nil {
		displayName = *extracted.PluginDisplayName
	}
	if name != nil {
		displayName = *name
	}
	newIid := nextChannelIid(project.Events)
	if newIid > 0xffff {
		mutErr("EVENT_NOT_FOUND", "cannot allocate new channel iid: max u16 (%d) reached", 0xffff)
	}
	insertAt := findInsertionBeforeArrangements(project.Events)
	maxF9, maxF10 := scanChannelD4UidMaxes(project.Events)
	newF9, newF10 := maxF9+1, maxF10+1
	sawKind, sawName := false, false
	patched := []FLPEvent{}
	for _, ev := range extracted.Scope {
		if ev.Kind == KindU8 && ev.Opcode == opChannelTypeEvt {
			sawKind = true
			patched = append(patched, ev)
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opName {
			sawName = true
			patched = append(patched, evBlob(opName, encodeUtf16LeNullTerminated(displayName)))
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginRuntime && len(ev.Payload) >= 44 {
			np := copyBytes(ev.Payload)
			le.PutUint32(np[36:], newF9)
			le.PutUint32(np[40:], newF10)
			patched = append(patched, evBlob(opPluginRuntime, np))
			continue
		}
		if ev.Kind == KindBlob {
			patched = append(patched, evBlob(ev.Opcode, copyBytes(ev.Payload)))
		} else {
			patched = append(patched, ev)
		}
	}
	prelude := []FLPEvent{evU16(opNewChannel, newIid)}
	if !sawKind {
		prelude = append(prelude, evU8(opChannelTypeEvt, 1))
	}
	if !sawName {
		prelude = append(prelude, evBlob(opName, encodeUtf16LeNullTerminated(displayName)))
	}
	events := insertEvents(cloneEvents(project), insertAt, append(prelude, patched...)...)
	bindUIDToFirstUnusedTrack(events, newF9)
	return withEvents(project, events), newIid, nil
}

// LoadFactoryEffectPreset splices a factory .fst effect preset into a mixer slot.
func LoadFactoryEffectPreset(project, donor *FLPProject, insertIndex, slotMarker int) (res *FLPProject, flIPCSlot int, err error) {
	defer catchMut(&err)
	extracted, e := ExtractPluginScopeFromFst(donor)
	if e != nil {
		if fe, ok := e.(*FstParseError); ok {
			mutErr("PLUGIN_INSTANTIATE_FAILED", "donor .fst: %s", fe.Message)
		}
		panic(e)
	}
	mutated, e := CraftPluginFixture(project, extracted.Scope, insertIndex, slotMarker)
	if e != nil {
		mutErr("PLUGIN_INSTANTIATE_FAILED", "splice into insert=%d slot_marker=%d: %s", insertIndex, slotMarker, e.Error())
	}
	return mutated, slotMarker + 1, nil
}

// SetChannelSamplePath sets (or inserts) the sample path event of a channel.
func SetChannelSamplePath(project *FLPProject, iid int, samplePath string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 0 {
		mutErr("INVALID_ARGS", "channel iid must be a non-negative integer, got %d", iid)
	}
	if samplePath == "" {
		mutErr("INVALID_ARGS", "samplePath must be a non-empty string")
	}
	events := cloneEvents(project)
	payload := encodeUtf16LeNullTerminated(samplePath)
	open := findOpen(events, KindU16, opNewChannel, iid)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no channel with iid=%d found", iid)
	}
	end := findFirstAfter(events, open, channelEndPred)
	for i := open + 1; i < end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opSamplePath {
			events[i] = evBlob(opSamplePath, payload)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, open+1, evBlob(opSamplePath, payload))
	return withEvents(project, events), nil
}

var _ = fmt.Sprintf
