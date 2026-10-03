package flp

import (
	"math"
	"sort"
)

const (
	opPlaylistEvt  = 0xe9
	clipPatternBase = 20480
	clipTrackMax    = 499
	clipRecordSize  = 80
)

// ClipPlacement describes a new playlist clip. Numeric fields are float64 so
// non-integer input is rejected with INVALID_ARGS, as the original does.
type ClipPlacement struct {
	Kind          string // "pattern" | "channel"
	RefID         float64
	TrackIndex    float64
	PositionTicks float64
	LengthTicks   float64
}

// ClipMatch selects existing clips.
type ClipMatch struct {
	TrackIndex    float64
	PositionTicks *float64
	RefID         *float64
	Kind          string // "", "pattern" or "channel"
}

func encodeClipRecord(position, itemIndex, length, trackRvidx, clipID uint32) []byte {
	buf := make([]byte, clipRecordSize)
	le.PutUint32(buf[0:], position)
	le.PutUint16(buf[4:], clipPatternBase)
	le.PutUint16(buf[6:], uint16(itemIndex))
	le.PutUint32(buf[8:], length)
	le.PutUint16(buf[12:], uint16(trackRvidx))
	le.PutUint16(buf[14:], 0)
	le.PutUint16(buf[16:], 120)
	le.PutUint16(buf[18:], 64)
	buf[20] = 64
	buf[21] = 100
	buf[22] = 128
	buf[23] = 128
	for i := 24; i < 32; i++ {
		buf[i] = 0xff
	}
	le.PutUint32(buf[32:], clipID)
	le.PutUint64(buf[64:], math.Float64bits(1.0))
	return buf
}

func maxClipID(payload []byte) uint32 {
	if len(payload) == 0 || len(payload)%clipRecordSize != 0 {
		return 0
	}
	var max uint32
	for p := 0; p+clipRecordSize <= len(payload); p += clipRecordSize {
		if id := le.Uint32(payload[p+32:]); id > max {
			max = id
		}
	}
	return max
}

func resolveItemIndex(pl ClipPlacement) uint32 {
	if pl.Kind == "pattern" {
		if !isInt(pl.RefID) || pl.RefID < 1 {
			mutErr("INVALID_ARGS", "pattern ref_id must be a positive integer, got %s", jsNum(pl.RefID))
		}
		return uint32(pl.RefID) + clipPatternBase
	}
	if !isInt(pl.RefID) || pl.RefID < 0 {
		mutErr("INVALID_ARGS", "channel ref_id must be a non-negative integer, got %s", jsNum(pl.RefID))
	}
	return uint32(pl.RefID)
}

func resolveTrackRvidx(trackIndex float64) uint32 {
	if !isInt(trackIndex) || trackIndex < 0 || trackIndex > clipTrackMax {
		mutErr("INVALID_ARGS", "track_index must be integer in [0, %d], got %s", clipTrackMax, jsNum(trackIndex))
	}
	return uint32(clipTrackMax - int(trackIndex))
}

// AddClip appends a playlist clip to an arrangement.
func AddClip(project *FLPProject, arrangementID int, pl ClipPlacement) (res *FLPProject, err error) {
	defer catchMut(&err)
	if !isInt(pl.PositionTicks) || pl.PositionTicks < 0 {
		mutErr("INVALID_ARGS", "position_ticks must be non-negative integer")
	}
	if !isInt(pl.LengthTicks) || pl.LengthTicks < 1 {
		mutErr("INVALID_ARGS", "length_ticks must be positive integer")
	}
	itemIndex := resolveItemIndex(pl)
	trackRvidx := resolveTrackRvidx(pl.TrackIndex)
	events := cloneEvents(project)
	open, end := arrangementRange(events, arrangementID)
	lastBlob := -1
	var maxID uint32
	for i := open + 1; i < end; i++ {
		ev := events[i]
		if ev.Kind == KindBlob && ev.Opcode == opPlaylistEvt {
			lastBlob = i
			if id := maxClipID(ev.Payload); id > maxID {
				maxID = id
			}
		}
	}
	rec := encodeClipRecord(uint32(pl.PositionTicks), itemIndex, uint32(pl.LengthTicks), trackRvidx, maxID+1)
	if lastBlob == -1 {
		events = insertEvents(events, end, evBlob(opPlaylistEvt, rec))
		return withEvents(project, events), nil
	}
	merged := append(copyBytes(events[lastBlob].Payload), rec...)
	events[lastBlob] = evBlob(opPlaylistEvt, merged)
	return withEvents(project, events), nil
}

func clipMatches(m ClipMatch, recPos uint32, recItem int, recTrackRv uint32) bool {
	if resolveTrackRvidx(m.TrackIndex) != recTrackRv {
		return false
	}
	if m.PositionTicks != nil && *m.PositionTicks != float64(recPos) {
		return false
	}
	if m.RefID != nil {
		isPattern := recItem > clipPatternBase
		recordRef := recItem
		if isPattern {
			recordRef = recItem - clipPatternBase
		}
		if m.Kind != "" {
			expected := "channel"
			if isPattern {
				expected = "pattern"
			}
			if m.Kind != expected {
				return false
			}
		}
		if *m.RefID != float64(recordRef) {
			return false
		}
	}
	return true
}

func playlistRecordSize(n int) int {
	switch {
	case n%80 == 0:
		return 80
	case n%60 == 0:
		return 60
	case n%32 == 0:
		return 32
	}
	return 0
}

// RemoveClip deletes matching playlist clips.
func RemoveClip(project *FLPProject, arrangementID int, m ClipMatch) (res *FLPProject, err error) {
	defer catchMut(&err)
	events := cloneEvents(project)
	open, end := arrangementRange(events, arrangementID)
	touched := false
	for i := open + 1; i < end && i < len(events); i++ {
		ev := events[i]
		if ev.Kind != KindBlob || ev.Opcode != opPlaylistEvt {
			continue
		}
		rs := playlistRecordSize(len(ev.Payload))
		if rs == 0 {
			continue
		}
		keep := []byte{}
		dropped := false
		for p := 0; p+rs <= len(ev.Payload); p += rs {
			pos := le.Uint32(ev.Payload[p:])
			item := int(le.Uint16(ev.Payload[p+6:]))
			trk := uint32(le.Uint16(ev.Payload[p+12:]))
			if clipMatches(m, pos, item, trk) {
				dropped = true
				continue
			}
			keep = append(keep, ev.Payload[p:p+rs]...)
		}
		if !dropped {
			continue
		}
		touched = true
		if len(keep) == 0 {
			events = append(events[:i], events[i+1:]...)
			i--
			end--
			continue
		}
		events[i] = evBlob(opPlaylistEvt, keep)
	}
	if !touched {
		mutErr("EVENT_NOT_FOUND", "no clips matched in arrangement %d", arrangementID)
	}
	return withEvents(project, events), nil
}

// ClipDest is the destination for MoveClip; nil fields are left unchanged.
type ClipDest struct {
	TrackIndex    *float64
	PositionTicks *float64
}

// MoveClip moves matching clips to a new track and/or position.
func MoveClip(project *FLPProject, arrangementID int, m ClipMatch, to ClipDest) (res *FLPProject, err error) {
	defer catchMut(&err)
	if to.TrackIndex == nil && to.PositionTicks == nil {
		mutErr("INVALID_ARGS", "moveClip requires at least one of {track_index, position_ticks}")
	}
	if to.PositionTicks != nil && (!isInt(*to.PositionTicks) || *to.PositionTicks < 0) {
		mutErr("INVALID_ARGS", "to.position_ticks must be non-negative integer")
	}
	var newRv *uint32
	if to.TrackIndex != nil {
		v := resolveTrackRvidx(*to.TrackIndex)
		newRv = &v
	}
	events := cloneEvents(project)
	open, end := arrangementRange(events, arrangementID)
	touched := false
	for i := open + 1; i < end; i++ {
		ev := events[i]
		if ev.Kind != KindBlob || ev.Opcode != opPlaylistEvt {
			continue
		}
		rs := playlistRecordSize(len(ev.Payload))
		if rs == 0 {
			continue
		}
		np := copyBytes(ev.Payload)
		blobTouched := false
		for p := 0; p+rs <= len(np); p += rs {
			pos := le.Uint32(np[p:])
			item := int(le.Uint16(np[p+6:]))
			trk := uint32(le.Uint16(np[p+12:]))
			if !clipMatches(m, pos, item, trk) {
				continue
			}
			if to.PositionTicks != nil {
				le.PutUint32(np[p:], uint32(*to.PositionTicks))
			}
			if newRv != nil {
				le.PutUint16(np[p+12:], uint16(*newRv))
			}
			blobTouched = true
			touched = true
		}
		if blobTouched {
			events[i] = evBlob(opPlaylistEvt, np)
		}
	}
	if !touched {
		mutErr("EVENT_NOT_FOUND", "no clips matched in arrangement %d", arrangementID)
	}
	return withEvents(project, events), nil
}

// ───────────────────────── notes ─────────────────────────

const noteRecordSize = 24

// NoteInput is a note for mutation APIs; float64 fields let invalid input be
// rejected with the same messages as the original.
type NoteInput struct {
	Position, ChannelIid, Length, Key, Flags, Group float64
	FinePitch, Release, MidiChannel, Pan, Velocity  float64
	ModX, ModY                                      float64
}

// NoteToInput converts a decoded Note into a NoteInput.
func NoteToInput(n Note) NoteInput {
	return NoteInput{
		Position: float64(n.Position), ChannelIid: float64(n.ChannelIid), Length: float64(n.Length), Key: float64(n.Key),
		Flags: float64(n.Flags), Group: float64(n.Group), FinePitch: float64(n.FinePitch), Release: float64(n.Release),
		MidiChannel: float64(n.MidiChan), Pan: float64(n.Pan), Velocity: float64(n.Velocity), ModX: float64(n.ModX), ModY: float64(n.ModY),
	}
}

// toInt32 mimics JavaScript's ToInt32.
func toInt32(f float64) int32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	t := math.Trunc(f)
	m := math.Mod(t, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return int32(uint32(m))
}

// EncodeNote encodes a note into its 24-byte record.
func EncodeNote(n NoteInput) []byte {
	if !isInt(n.Position) || n.Position < 0 || n.Position > 0xffffffff {
		mutErr("INVALID_ARGS", "note.position must be u32, got %s", jsNum(n.Position))
	}
	if !isInt(n.Length) || n.Length < 0 || n.Length > 0xffffffff {
		mutErr("INVALID_ARGS", "note.length must be u32, got %s", jsNum(n.Length))
	}
	if !isInt(n.ChannelIid) || n.ChannelIid < 0 || n.ChannelIid > 0xffff {
		mutErr("INVALID_ARGS", "note.channel_iid must be u16 (0..65535), got %s", jsNum(n.ChannelIid))
	}
	if !isInt(n.Key) || n.Key < 0 || n.Key > 131 {
		mutErr("INVALID_ARGS", "note.key must be in [0, 131] (FL MIDI range), got %s", jsNum(n.Key))
	}
	buf := make([]byte, noteRecordSize)
	le.PutUint32(buf[0:], uint32(n.Position))
	le.PutUint16(buf[4:], uint16(toInt32(n.Flags)&0xffff))
	le.PutUint16(buf[6:], uint16(n.ChannelIid))
	le.PutUint32(buf[8:], uint32(n.Length))
	le.PutUint16(buf[12:], uint16(n.Key))
	le.PutUint16(buf[14:], uint16(toInt32(n.Group)&0xffff))
	buf[16] = byte(toInt32(n.FinePitch) & 0xff)
	buf[18] = byte(toInt32(n.Release) & 0xff)
	buf[19] = byte(toInt32(n.MidiChannel) & 0xff)
	buf[20] = byte(toInt32(n.Pan) & 0xff)
	buf[21] = byte(toInt32(n.Velocity) & 0xff)
	buf[22] = byte(toInt32(n.ModX) & 0xff)
	buf[23] = byte(toInt32(n.ModY) & 0xff)
	return buf
}

func encodePatternNotes(notes []NoteInput) []byte {
	sorted := make([]NoteInput, len(notes))
	copy(sorted, notes)
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a].Position != sorted[b].Position {
			return sorted[a].Position < sorted[b].Position
		}
		return sorted[a].ChannelIid < sorted[b].ChannelIid
	})
	out := make([]byte, 0, len(sorted)*noteRecordSize)
	for _, n := range sorted {
		out = append(out, EncodeNote(n)...)
	}
	return out
}

type patternScope struct{ start, end int }

func findPatternScope(events []FLPEvent, patternID int) *patternScope {
	start := -1
	for i, ev := range events {
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew && int(ev.Value) == patternID {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	for i := start + 1; i < len(events); i++ {
		ev := events[i]
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew {
			return &patternScope{start, i}
		}
		if ev.Kind == KindU16 && ev.Opcode == opArrangementNew {
			return &patternScope{start, i}
		}
	}
	return &patternScope{start, len(events)}
}

func collectExistingNotes(events []FLPEvent, sc *patternScope) []Note {
	out := []Note{}
	for i := sc.start + 1; i < sc.end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opPatternNotes {
			out = append(out, decodeNotes(events[i].Payload)...)
		}
	}
	return out
}

func checkPatternID(patternID int) {
	if patternID < 1 {
		mutErr("INVALID_ARGS", "pattern id must be a positive integer (1-based), got %d", patternID)
	}
}

func mustScope(events []FLPEvent, patternID int) *patternScope {
	sc := findPatternScope(events, patternID)
	if sc == nil {
		mutErr("EVENT_NOT_FOUND", "no pattern with id=%d found", patternID)
	}
	return sc
}

// SetPatternNotes replaces every note in a pattern.
func SetPatternNotes(project *FLPProject, patternID int, notes []NoteInput) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	var encoded []byte
	if len(notes) > 0 {
		encoded = encodePatternNotes(notes)
	}
	events := make([]FLPEvent, 0, len(project.Events))
	for i, ev := range project.Events {
		if i > sc.start && i < sc.end && ev.Opcode == opPatternNotes {
			continue
		}
		events = append(events, ev)
	}
	if encoded == nil {
		return withEvents(project, events), nil
	}
	ns := findPatternScope(events, patternID)
	insertAt := ns.start + 1
	for i := ns.start + 1; i < ns.end; i++ {
		if events[i].Opcode == opPatternName {
			insertAt = i + 1
			break
		}
	}
	events = insertEvents(events, insertAt, evBlob(opPatternNotes, encoded))
	return withEvents(project, events), nil
}

// AddPatternNote appends a note to a pattern.
func AddPatternNote(project *FLPProject, patternID int, note NoteInput) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	EncodeNote(note)
	existing := collectExistingNotes(project.Events, sc)
	all := make([]NoteInput, 0, len(existing)+1)
	for _, n := range existing {
		all = append(all, NoteToInput(n))
	}
	all = append(all, note)
	return SetPatternNotes(project, patternID, all)
}

// RemovePatternNote removes the note at index.
func RemovePatternNote(project *FLPProject, patternID int, index float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	existing := collectExistingNotes(project.Events, sc)
	if !isInt(index) || index < 0 || index >= float64(len(existing)) {
		mutErr("INVALID_ARGS", "note index %s out of range [0, %d)", jsNum(index), len(existing))
	}
	kept := []NoteInput{}
	for i, n := range existing {
		if float64(i) != index {
			kept = append(kept, NoteToInput(n))
		}
	}
	return SetPatternNotes(project, patternID, kept)
}

// RemovePatternNotesWhere removes every note for which pred returns true.
func RemovePatternNotesWhere(project *FLPProject, patternID int, pred func(Note, int) bool) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	existing := collectExistingNotes(project.Events, sc)
	kept := []NoteInput{}
	for i, n := range existing {
		if !pred(n, i) {
			kept = append(kept, NoteToInput(n))
		}
	}
	return SetPatternNotes(project, patternID, kept)
}

// ───────────────────────── controllers ─────────────────────────

const controllerRecordSize = 12

// ControllerInput is a pattern controller for mutation APIs.
type ControllerInput struct {
	Position, Channel, Flags, Value float64
}

// EncodeController encodes a 12-byte controller record.
func EncodeController(c ControllerInput) []byte {
	if !isInt(c.Position) || c.Position < 0 || c.Position > 0xffffffff {
		mutErr("INVALID_ARGS", "controller.position must be u32, got %s", jsNum(c.Position))
	}
	if !isInt(c.Channel) || c.Channel < 0 || c.Channel > 255 {
		mutErr("INVALID_ARGS", "controller.channel must be u8 (0..255), got %s", jsNum(c.Channel))
	}
	if !isInt(c.Flags) || c.Flags < 0 || c.Flags > 255 {
		mutErr("INVALID_ARGS", "controller.flags must be u8 (0..255), got %s", jsNum(c.Flags))
	}
	if math.IsNaN(c.Value) || math.IsInf(c.Value, 0) {
		mutErr("INVALID_ARGS", "controller.value must be a finite float, got %s", jsNum(c.Value))
	}
	buf := make([]byte, controllerRecordSize)
	le.PutUint32(buf[0:], uint32(c.Position))
	buf[6] = byte(c.Channel)
	buf[7] = byte(c.Flags)
	le.PutUint32(buf[8:], math.Float32bits(float32(c.Value)))
	return buf
}

func controllerToInput(c Controller) ControllerInput {
	return ControllerInput{Position: float64(c.Position), Channel: float64(c.Channel), Flags: float64(c.Flags), Value: float64(c.Value)}
}

func collectExistingControllers(events []FLPEvent, sc *patternScope) []Controller {
	out := []Controller{}
	for i := sc.start + 1; i < sc.end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opPatternCtrls {
			out = append(out, decodeControllers(events[i].Payload)...)
		}
	}
	return out
}

// SetPatternControllers replaces every controller in a pattern.
func SetPatternControllers(project *FLPProject, patternID int, ctrls []ControllerInput) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	var encoded []byte
	if len(ctrls) > 0 {
		encoded = make([]byte, 0, len(ctrls)*controllerRecordSize)
		for _, c := range ctrls {
			encoded = append(encoded, EncodeController(c)...)
		}
	}
	events := make([]FLPEvent, 0, len(project.Events))
	for i, ev := range project.Events {
		if i > sc.start && i < sc.end && ev.Opcode == opPatternCtrls {
			continue
		}
		events = append(events, ev)
	}
	if encoded == nil {
		return withEvents(project, events), nil
	}
	ns := findPatternScope(events, patternID)
	insertAt := ns.start + 1
	for i := ns.start + 1; i < ns.end; i++ {
		if events[i].Opcode == opPatternName {
			insertAt = i + 1
			break
		}
	}
	events = insertEvents(events, insertAt, evBlob(opPatternCtrls, encoded))
	return withEvents(project, events), nil
}

// AddPatternController appends a controller to a pattern.
func AddPatternController(project *FLPProject, patternID int, c ControllerInput) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	EncodeController(c)
	existing := collectExistingControllers(project.Events, sc)
	all := make([]ControllerInput, 0, len(existing)+1)
	for _, e := range existing {
		all = append(all, controllerToInput(e))
	}
	all = append(all, c)
	return SetPatternControllers(project, patternID, all)
}

// RemovePatternController removes the controller at index.
func RemovePatternController(project *FLPProject, patternID int, index float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkPatternID(patternID)
	sc := mustScope(project.Events, patternID)
	existing := collectExistingControllers(project.Events, sc)
	if !isInt(index) || index < 0 || index >= float64(len(existing)) {
		mutErr("INVALID_ARGS", "controller index %s out of range [0, %d)", jsNum(index), len(existing))
	}
	kept := []ControllerInput{}
	for i, e := range existing {
		if float64(i) != index {
			kept = append(kept, controllerToInput(e))
		}
	}
	return SetPatternControllers(project, patternID, kept)
}
