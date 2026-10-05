package flpdiff

import (
	"fmt"
	"math"
)

// MutationError is returned by every mutation on invalid input or missing targets.
type MutationError struct {
	Code    string
	Message string
}

func (e *MutationError) Error() string { return e.Message }

// mutErr panics with a MutationError; exported mutations recover it via catchMut.
func mutErr(code, format string, a ...interface{}) {
	panic(&MutationError{Code: code, Message: fmt.Sprintf(format, a...)})
}

// catchMut converts a MutationError panic into a returned error.
func catchMut(err *error) {
	if r := recover(); r != nil {
		if me, ok := r.(*MutationError); ok {
			*err = me
			return
		}
		panic(r)
	}
}

// must unwraps a (project, error) pair inside a mutation, re-panicking on error.
func must(p *FLPProject, err error) *FLPProject {
	if err != nil {
		panic(err)
	}
	return p
}

// MutRGBA is a colour for mutations; components are validated as integers in [0,255].
type MutRGBA struct {
	R, G, B, A float64
}

func packRGBA(c MutRGBA) uint32 {
	for _, nv := range []struct {
		n string
		v float64
	}{{"r", c.R}, {"g", c.G}, {"b", c.B}, {"a", c.A}} {
		if nv.v != math.Trunc(nv.v) || math.IsInf(nv.v, 0) || math.IsNaN(nv.v) || nv.v < 0 || nv.v > 255 {
			mutErr("INVALID_ARGS", "RGBA.%s must be integer in [0, 255], got %s", nv.n, jsNum(nv.v))
		}
	}
	return uint32(c.A)<<24 | uint32(c.B)<<16 | uint32(c.G)<<8 | uint32(c.R)
}

func evU8(op int, v int) FLPEvent     { return FLPEvent{Kind: KindU8, Opcode: op, Value: uint32(v)} }
func evU16(op int, v int) FLPEvent    { return FLPEvent{Kind: KindU16, Opcode: op, Value: uint32(v)} }
func evU32(op int, v uint32) FLPEvent { return FLPEvent{Kind: KindU32, Opcode: op, Value: v} }
func evBlob(op int, p []byte) FLPEvent {
	return FLPEvent{Kind: KindBlob, Opcode: op, Payload: p}
}

// encodeUtf16LeNullTerminated encodes s as UTF-16LE with a 2-byte terminator.
func encodeUtf16LeNullTerminated(s string) []byte {
	b := stringToUTF16LE(s)
	return append(b, 0, 0)
}

func cloneEvents(p *FLPProject) []FLPEvent {
	out := make([]FLPEvent, len(p.Events))
	copy(out, p.Events)
	return out
}

func withEvents(p *FLPProject, events []FLPEvent) *FLPProject {
	np := *p
	np.Events = events
	return &np
}

func insertEvents(events []FLPEvent, idx int, ins ...FLPEvent) []FLPEvent {
	out := make([]FLPEvent, 0, len(events)+len(ins))
	out = append(out, events[:idx]...)
	out = append(out, ins...)
	out = append(out, events[idx:]...)
	return out
}

func copyBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func isInt(f float64) bool { return f == math.Trunc(f) && !math.IsInf(f, 0) && !math.IsNaN(f) }

// findOpen returns the index of the first event of (kind, opcode, value), or -1.
func findOpen(events []FLPEvent, kind EventKind, op int, value int) int {
	for i, ev := range events {
		if ev.Kind == kind && ev.Opcode == op && int(ev.Value) == value {
			return i
		}
	}
	return -1
}

// findFirstAfter returns the first index > from where pred holds, else len(events).
func findFirstAfter(events []FLPEvent, from int, pred func(FLPEvent) bool) int {
	for i := from + 1; i < len(events); i++ {
		if pred(events[i]) {
			return i
		}
	}
	return len(events)
}

func channelEndPred(ev FLPEvent) bool {
	return (ev.Kind == KindU16 && ev.Opcode == opNewChannel) ||
		(ev.Kind == KindU32 && ev.Opcode == opInsertEnd) ||
		(ev.Kind == KindBlob && ev.Opcode == opInsertFlags)
}

// arrangementRange returns (openIndex, endIndex) for the arrangement with the given id.
func arrangementRange(events []FLPEvent, arrangementID int) (int, int) {
	open := findOpen(events, KindU16, opArrangementNew, arrangementID)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no arrangement with id=%d found", arrangementID)
	}
	end := findFirstAfter(events, open, func(ev FLPEvent) bool { return ev.Kind == KindU16 && ev.Opcode == opArrangementNew })
	return open, end
}

// trackBlobIndex returns the index of the trackIndex-th 0xEE blob in (open,end).
func trackBlobIndex(events []FLPEvent, arrangementID, trackIndex, open, end int) int {
	seen := -1
	for i := open + 1; i < end; i++ {
		ev := events[i]
		if ev.Kind == KindBlob && ev.Opcode == opTrackData {
			seen++
			if seen == trackIndex {
				return i
			}
		}
	}
	mutErr("EVENT_NOT_FOUND", "arrangement %d has no track at index %d (only %d tracks)", arrangementID, trackIndex, seen+1)
	return -1
}

func checkArrTrack(arrangementID, trackIndex int) {
	if arrangementID < 0 {
		mutErr("INVALID_ARGS", "arrangement id must be a non-negative integer, got %d", arrangementID)
	}
	if trackIndex < 0 {
		mutErr("INVALID_ARGS", "track index must be a non-negative integer, got %d", trackIndex)
	}
}

// ───────────────────────── simple scalar setters ─────────────────────────

const opTempoU32 = 0x9c

// SetTempo rewrites the modern 0x9C tempo event.
func SetTempo(project *FLPProject, bpm float64) (res *FLPProject, err error) {
	defer catchMut(&err)
	if math.IsNaN(bpm) || math.IsInf(bpm, 0) || bpm <= 0 {
		mutErr("INVALID_ARGS", "bpm must be a positive number, got %s", jsNum(bpm))
	}
	milli := uint32(jsRound(bpm * 1000))
	events := cloneEvents(project)
	found := -1
	for i, ev := range events {
		if ev.Kind == KindU32 && ev.Opcode == opTempoU32 {
			found = i
			break
		}
	}
	if found == -1 {
		for _, e := range events {
			if e.Kind == KindU16 && (e.Opcode == 0x42 || e.Opcode == 0x5d) {
				mutErr("LEGACY_TEMPO_FORMAT", "this FLP uses the pre-FL-3.4.0 tempo opcodes (0x42 + 0x5D); set_tempo only writes the modern 0x9C u32 form")
			}
		}
		mutErr("EVENT_NOT_FOUND", "no tempo event (opcode 0x9C) in this project")
	}
	events[found] = evU32(opTempoU32, milli)
	return withEvents(project, events), nil
}

// SetPatternName renames pattern iid (1-based), inserting a name event if absent.
func SetPatternName(project *FLPProject, iid int, name string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 1 {
		mutErr("INVALID_ARGS", "pattern iid must be a positive integer (1-based), got %d", iid)
	}
	if name == "" {
		mutErr("INVALID_ARGS", "name must be a non-empty string")
	}
	events := cloneEvents(project)
	newPayload := encodeUtf16LeNullTerminated(name)
	currentID := -1
	patternFound, nameReplaced := false, false
	insertAfter := -1
	for i := 0; i < len(events); i++ {
		ev := events[i]
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew {
			if currentID == iid && !nameReplaced {
				insertAfter = i - 1
				break
			}
			currentID = int(ev.Value)
			if currentID == iid {
				patternFound = true
			}
			continue
		}
		if currentID == iid && ev.Kind == KindBlob && ev.Opcode == opPatternName {
			events[i] = evBlob(opPatternName, newPayload)
			nameReplaced = true
			patternFound = true
			break
		}
	}
	if !nameReplaced && currentID == iid && insertAfter == -1 {
		insertAfter = len(events) - 1
		patternFound = true
	}
	if !patternFound {
		mutErr("EVENT_NOT_FOUND", "no pattern with iid=%d found (max pattern id seen: %d)", iid, currentID)
	}
	if !nameReplaced {
		events = insertEvents(events, insertAfter+1, evBlob(opPatternName, newPayload))
	}
	return withEvents(project, events), nil
}

// SetChannelName renames channel iid.
func SetChannelName(project *FLPProject, iid int, name string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 0 {
		mutErr("INVALID_ARGS", "channel iid must be a non-negative integer, got %d", iid)
	}
	if name == "" {
		mutErr("INVALID_ARGS", "name must be a non-empty string")
	}
	events := cloneEvents(project)
	newPayload := encodeUtf16LeNullTerminated(name)
	open := findOpen(events, KindU16, opNewChannel, iid)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no channel with iid=%d found", iid)
	}
	end := findFirstAfter(events, open, channelEndPred)
	for i := open + 1; i < end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opName {
			events[i] = evBlob(opName, newPayload)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, open+1, evBlob(opName, newPayload))
	return withEvents(project, events), nil
}

// insertCloserRange returns (kthCloseIdx, prevCloseIdx) over 0x93 closers.
func insertCloserRange(events []FLPEvent, index int) (int, int) {
	kth, prev, seen := -1, -1, -1
	for i, ev := range events {
		if ev.Kind == KindU32 && ev.Opcode == opInsertEnd {
			seen++
			if seen == index {
				kth = i
				break
			}
			prev = i
		}
	}
	if kth == -1 {
		mutErr("EVENT_NOT_FOUND", "no mixer insert with index=%d found (saw %d 0x93 closers)", index, seen+1)
	}
	return kth, prev
}

// SetInsertName renames mixer insert index (empty string clears).
func SetInsertName(project *FLPProject, index int, name string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if index < 0 {
		mutErr("INVALID_ARGS", "insert index must be a non-negative integer, got %d", index)
	}
	events := cloneEvents(project)
	kth, prev := insertCloserRange(events, index)
	newPayload := encodeUtf16LeNullTerminated(name)
	for i := prev + 1; i < kth; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opInsertName {
			events[i] = evBlob(opInsertName, newPayload)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, kth, evBlob(opInsertName, newPayload))
	return withEvents(project, events), nil
}

// SetTimeSignature rewrites the project time-signature events.
func SetTimeSignature(project *FLPProject, numerator, denominator int) (res *FLPProject, err error) {
	defer catchMut(&err)
	if numerator < 1 || numerator > 255 {
		mutErr("INVALID_ARGS", "numerator must be an integer in [1, 255], got %d", numerator)
	}
	if denominator < 1 || denominator > 64 || (denominator&(denominator-1)) != 0 {
		mutErr("INVALID_ARGS", "denominator must be a power of 2 in [1, 64] (1, 2, 4, 8, 16, 32, 64), got %d", denominator)
	}
	events := cloneEvents(project)
	numIdx, denomIdx := -1, -1
	for i, ev := range events {
		if ev.Kind == KindU8 && ev.Opcode == opProjTimeNum && numIdx == -1 {
			numIdx = i
		} else if ev.Kind == KindU8 && ev.Opcode == opProjTimeDenom && denomIdx == -1 {
			denomIdx = i
		}
		if numIdx != -1 && denomIdx != -1 {
			break
		}
	}
	if numIdx == -1 {
		mutErr("EVENT_NOT_FOUND", "no project time-signature numerator (opcode 0x11) in this project")
	}
	if denomIdx == -1 {
		mutErr("EVENT_NOT_FOUND", "no project time-signature denominator (opcode 0x12) in this project")
	}
	events[numIdx] = evU8(opProjTimeNum, numerator)
	events[denomIdx] = evU8(opProjTimeDenom, denominator)
	return withEvents(project, events), nil
}

// setU32InBlock replaces or inserts a u32 colour event inside a block [open+1, end).
func setU32InBlock(events []FLPEvent, open, end, colorOp int, value uint32) []FLPEvent {
	for i := open + 1; i < end; i++ {
		if events[i].Kind == KindU32 && events[i].Opcode == colorOp {
			events[i] = evU32(colorOp, value)
			return events
		}
	}
	return insertEvents(events, open+1, evU32(colorOp, value))
}

// SetChannelColor sets channel iid's colour.
func SetChannelColor(project *FLPProject, iid int, rgba MutRGBA) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 0 {
		mutErr("INVALID_ARGS", "channel iid must be a non-negative integer, got %d", iid)
	}
	events := cloneEvents(project)
	value := packRGBA(rgba)
	open := findOpen(events, KindU16, opNewChannel, iid)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no channel with id=%d found", iid)
	}
	end := len(events)
	for i := open + 1; i < len(events); i++ {
		ev := events[i]
		if ev.Kind == KindU16 && ev.Opcode == opNewChannel {
			end = i
			break
		}
		if (ev.Kind == KindU32 && ev.Opcode == opInsertEnd) || (ev.Kind == KindBlob && ev.Opcode == opInsertFlags) {
			end = i
			break
		}
	}
	events = setU32InBlock(events, open, end, opPluginColor, value)
	return withEvents(project, events), nil
}

// SetInsertColor sets mixer insert index's colour.
func SetInsertColor(project *FLPProject, index int, rgba MutRGBA) (res *FLPProject, err error) {
	defer catchMut(&err)
	if index < 0 {
		mutErr("INVALID_ARGS", "insert index must be a non-negative integer, got %d", index)
	}
	events := cloneEvents(project)
	value := packRGBA(rgba)
	kth, prev := insertCloserRange(events, index)
	for i := prev + 1; i < kth; i++ {
		if events[i].Kind == KindU32 && events[i].Opcode == opInsertColor {
			events[i] = evU32(opInsertColor, value)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, kth, evU32(opInsertColor, value))
	return withEvents(project, events), nil
}

// SetPatternColor sets pattern iid's colour.
func SetPatternColor(project *FLPProject, iid int, rgba MutRGBA) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 1 {
		mutErr("INVALID_ARGS", "pattern iid must be a positive integer (1-based), got %d", iid)
	}
	events := cloneEvents(project)
	value := packRGBA(rgba)
	open := findOpen(events, KindU16, opPatternNew, iid)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no pattern with iid=%d found", iid)
	}
	end := findFirstAfter(events, open, func(ev FLPEvent) bool { return ev.Kind == KindU16 && ev.Opcode == opPatternNew })
	events = setU32InBlock(events, open, end, opPatternColor, value)
	return withEvents(project, events), nil
}

// SetChannelRouting sets the channel→insert routing (-1 = unrouted).
func SetChannelRouting(project *FLPProject, iid, targetInsert int) (res *FLPProject, err error) {
	defer catchMut(&err)
	if iid < 0 {
		mutErr("INVALID_ARGS", "channel iid must be a non-negative integer, got %d", iid)
	}
	if targetInsert < -1 || targetInsert > 127 {
		mutErr("INVALID_ARGS", "targetInsert must be integer in [-1, 127] (signed int8), got %d", targetInsert)
	}
	events := cloneEvents(project)
	encoded := targetInsert
	if encoded < 0 {
		encoded += 256
	}
	open := findOpen(events, KindU16, opNewChannel, iid)
	if open == -1 {
		mutErr("EVENT_NOT_FOUND", "no channel with iid=%d found", iid)
	}
	end := findFirstAfter(events, open, channelEndPred)
	for i := open + 1; i < end; i++ {
		if events[i].Kind == KindU8 && events[i].Opcode == opChannelRoutedTo {
			events[i] = evU8(opChannelRoutedTo, encoded)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, open+1, evU8(opChannelRoutedTo, encoded))
	return withEvents(project, events), nil
}

// SetArrangementName renames an arrangement.
func SetArrangementName(project *FLPProject, arrangementID int, name string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if arrangementID < 0 {
		mutErr("INVALID_ARGS", "arrangement id must be a non-negative integer, got %d", arrangementID)
	}
	if name == "" {
		mutErr("INVALID_ARGS", "name must be a non-empty string")
	}
	events := cloneEvents(project)
	payload := encodeUtf16LeNullTerminated(name)
	open, end := arrangementRange(events, arrangementID)
	for i := open + 1; i < end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opArrangementName {
			events[i] = evBlob(opArrangementName, payload)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, open+1, evBlob(opArrangementName, payload))
	return withEvents(project, events), nil
}

// SetTrackName renames the trackIndex-th track of an arrangement.
func SetTrackName(project *FLPProject, arrangementID, trackIndex int, name string) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkArrTrack(arrangementID, trackIndex)
	if name == "" {
		mutErr("INVALID_ARGS", "name must be a non-empty string")
	}
	events := cloneEvents(project)
	payload := encodeUtf16LeNullTerminated(name)
	open, end := arrangementRange(events, arrangementID)
	tb := trackBlobIndex(events, arrangementID, trackIndex, open, end)
	next := end
	for i := tb + 1; i < end; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opTrackData {
			next = i
			break
		}
	}
	for i := tb + 1; i < next; i++ {
		if events[i].Kind == KindBlob && events[i].Opcode == opTrackName {
			events[i] = evBlob(opTrackName, payload)
			return withEvents(project, events), nil
		}
	}
	events = insertEvents(events, tb+1, evBlob(opTrackName, payload))
	return withEvents(project, events), nil
}

// ClonePattern duplicates pattern sourceIid under a new id (max+1).
func ClonePattern(project *FLPProject, sourceIid int, newName *string) (res *FLPProject, err error) {
	defer catchMut(&err)
	if sourceIid < 1 {
		mutErr("INVALID_ARGS", "pattern iid must be a positive integer (1-based), got %d", sourceIid)
	}
	maxID := 0
	sawSource := false
	for _, ev := range project.Events {
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew {
			if int(ev.Value) > maxID {
				maxID = int(ev.Value)
			}
			if int(ev.Value) == sourceIid {
				sawSource = true
			}
		}
	}
	if !sawSource {
		mutErr("EVENT_NOT_FOUND", "no pattern with iid=%d found", sourceIid)
	}
	newID := maxID + 1
	scopeOps := map[int]bool{opPatternName: true, opPatternNotes: true, opPatternCtrls: true, opPatternColor: true, opPatternLength: true, opPatternLooped: true}
	finalName := fmt.Sprintf("Pattern %d copy", sourceIid)
	if newName != nil {
		finalName = *newName
	}
	cloned := []FLPEvent{}
	scopeID := -1
	lastSourceIdx := -1
	for i, ev := range project.Events {
		if ev.Kind == KindU16 && ev.Opcode == opPatternNew {
			scopeID = int(ev.Value)
			if scopeID == sourceIid {
				cloned = append(cloned, evU16(opPatternNew, newID))
				lastSourceIdx = i
			}
			continue
		}
		if scopeID != sourceIid {
			continue
		}
		if !scopeOps[ev.Opcode] {
			continue
		}
		lastSourceIdx = i
		if ev.Kind == KindBlob && ev.Opcode == opPatternName {
			cloned = append(cloned, evBlob(opPatternName, encodeUtf16LeNullTerminated(finalName)))
			continue
		}
		if ev.Kind == KindBlob {
			cloned = append(cloned, evBlob(ev.Opcode, copyBytes(ev.Payload)))
		} else {
			cloned = append(cloned, ev)
		}
	}
	hasName := false
	for _, e := range cloned {
		if e.Kind == KindBlob && e.Opcode == opPatternName {
			hasName = true
			break
		}
	}
	if !hasName {
		cloned = insertEvents(cloned, 1, evBlob(opPatternName, encodeUtf16LeNullTerminated(finalName)))
	}
	events := insertEvents(cloneEvents(project), lastSourceIdx+1, cloned...)
	return withEvents(project, events), nil
}

// SetTrackColor sets the colour in a track-data blob.
func SetTrackColor(project *FLPProject, arrangementID, trackIndex int, rgba MutRGBA) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkArrTrack(arrangementID, trackIndex)
	color := packRGBA(rgba)
	events := cloneEvents(project)
	open, end := arrangementRange(events, arrangementID)
	tb := trackBlobIndex(events, arrangementID, trackIndex, open, end)
	orig := events[tb]
	if orig.Kind != KindBlob || len(orig.Payload) < 8 {
		n := 0
		if orig.Kind == KindBlob {
			n = len(orig.Payload)
		}
		mutErr("INVALID_ARGS", "track-data blob too small (%d bytes); needs >= 8 for color", n)
	}
	p := copyBytes(orig.Payload)
	le.PutUint32(p[4:], color)
	events[tb] = evBlob(opTrackData, p)
	return withEvents(project, events), nil
}

// SetTrackGrouped sets the "grouped with track above" flag (blob byte 46).
func SetTrackGrouped(project *FLPProject, arrangementID, trackIndex int, grouped bool) (res *FLPProject, err error) {
	defer catchMut(&err)
	checkArrTrack(arrangementID, trackIndex)
	events := cloneEvents(project)
	open, end := arrangementRange(events, arrangementID)
	tb := trackBlobIndex(events, arrangementID, trackIndex, open, end)
	orig := events[tb]
	if orig.Kind != KindBlob || len(orig.Payload) < 47 {
		n := 0
		if orig.Kind == KindBlob {
			n = len(orig.Payload)
		}
		mutErr("INVALID_ARGS", "track-data blob too small for grouped flag (%d bytes; need >= 47)", n)
	}
	p := copyBytes(orig.Payload)
	if grouped {
		p[46] = 1
	} else {
		p[46] = 0
	}
	events[tb] = evBlob(opTrackData, p)
	return withEvents(project, events), nil
}
