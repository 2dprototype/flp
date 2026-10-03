package flp

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// DescribePositionDelta renders a tick shift in musical units.
func DescribePositionDelta(deltaTicks int, ppq int) string {
	if deltaTicks == 0 {
		return "no move"
	}
	direction := "earlier"
	if deltaTicks > 0 {
		direction = "later"
	}
	absTicks := deltaTicks
	if absTicks < 0 {
		absTicks = -absTicks
	}
	if ppq > 0 && absTicks%ppq == 0 {
		beats := absTicks / ppq
		s := ""
		if beats != 1 {
			s = "s"
		}
		return fmt.Sprintf("%d beat%s %s", beats, s, direction)
	}
	for _, divisor := range []int{2, 4, 8, 16, 32, 64} {
		if ppq%divisor != 0 {
			continue
		}
		unitTicks := ppq / divisor
		if unitTicks <= 0 {
			continue
		}
		if absTicks%unitTicks == 0 {
			count := absTicks / unitTicks
			if count == 1 {
				return fmt.Sprintf("1/%d beat %s", divisor, direction)
			}
			return fmt.Sprintf("%d/%d beat %s", count, divisor, direction)
		}
	}
	s := ""
	if absTicks != 1 {
		s = "s"
	}
	return fmt.Sprintf("%d tick%s %s", absTicks, s, direction)
}

// NotePitchLabel renders a MIDI-like key as scientific pitch notation.
func NotePitchLabel(key int) string {
	names := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := int(math.Floor(float64(key) / 12))
	idx := ((key % 12) + 12) % 12
	return fmt.Sprintf("%s%d", names[idx], octave)
}

func noteDescription(n NoteJson, ppq int) string {
	beat := float64(n.Position) / float64(ppq)
	return fmt.Sprintf("%s on channel %d at beat %s", NotePitchLabel(n.Key), n.ChannelIid, PythonGFormat(beat))
}

// PythonGFormat mimics Python's {:g} (6 significant digits, banker's rounding).
func PythonGFormat(n float64) string {
	if n == math.Trunc(n) && !math.IsInf(n, 0) {
		return jsNum(n)
	}
	if n == 0 {
		return "0"
	}
	exp := math.Floor(math.Log10(math.Abs(n)))
	mult := math.Pow(10, 5-exp)
	rounded := pythonRound(n*mult) / mult
	return jsNum(rounded)
}

func noteExactKey(n NoteJson) string {
	return fmt.Sprintf("%d\x00%d\x00%d", n.ChannelIid, n.Position, n.Key)
}

func noteMoveKey(n NoteJson) string {
	return fmt.Sprintf("%d\x00%d", n.ChannelIid, n.Key)
}

func notesFullyEqual(a, b NoteJson) bool {
	return a.Position == b.Position && a.Length == b.Length && a.Key == b.Key && a.ChannelIid == b.ChannelIid &&
		a.Pan == b.Pan && a.Velocity == b.Velocity && a.FinePitch == b.FinePitch && a.Release == b.Release
}

// DiffNotes produces the per-note diff between two note collections.
func DiffNotes(oldNotes, newNotes []NoteJson, ppq int) []NoteChange {
	changes := []NoteChange{}

	oldByExact := map[string][]int{}
	for i, n := range oldNotes {
		k := noteExactKey(n)
		oldByExact[k] = append(oldByExact[k], i)
	}
	consumedOld := map[int]bool{}
	consumedNew := map[int]bool{}

	exactMods := []NoteChange{}
	for j := range newNotes {
		newNote := newNotes[j]
		bucket, ok := oldByExact[noteExactKey(newNote)]
		if !ok {
			continue
		}
		oldIdx := -1
		for _, idx := range bucket {
			if !consumedOld[idx] {
				oldIdx = idx
				break
			}
		}
		if oldIdx < 0 {
			continue
		}
		consumedOld[oldIdx] = true
		consumedNew[j] = true
		oldNote := oldNotes[oldIdx]
		if notesFullyEqual(oldNote, newNote) {
			continue
		}
		exactMods = append(exactMods, buildNoteModified(oldNote, newNote, ppq))
	}
	sort.SliceStable(exactMods, func(a, b int) bool {
		ao, bo := exactMods[a].OldNote, exactMods[b].OldNote
		if ao.ChannelIid != bo.ChannelIid {
			return ao.ChannelIid < bo.ChannelIid
		}
		if ao.Position != bo.Position {
			return ao.Position < bo.Position
		}
		return ao.Key < bo.Key
	})
	changes = append(changes, exactMods...)

	oldByMove := map[string][]int{}
	for i, n := range oldNotes {
		if consumedOld[i] {
			continue
		}
		k := noteMoveKey(n)
		oldByMove[k] = append(oldByMove[k], i)
	}

	type movePair struct {
		idx int
		nc  NoteChange
	}
	moves := []movePair{}
	for j := range newNotes {
		if consumedNew[j] {
			continue
		}
		newNote := newNotes[j]
		bucket, ok := oldByMove[noteMoveKey(newNote)]
		if !ok {
			continue
		}
		bestI := -1
		var bestDelta int64
		for _, i := range bucket {
			if consumedOld[i] {
				continue
			}
			d := int64(oldNotes[i].Position) - int64(newNote.Position)
			if d < 0 {
				d = -d
			}
			if bestI < 0 || d < bestDelta {
				bestDelta = d
				bestI = i
			}
		}
		if bestI < 0 {
			continue
		}
		consumedOld[bestI] = true
		consumedNew[j] = true
		moves = append(moves, movePair{bestI, buildNoteMoved(oldNotes[bestI], newNote, ppq)})
	}
	sort.SliceStable(moves, func(a, b int) bool { return moves[a].idx < moves[b].idx })
	for _, m := range moves {
		changes = append(changes, m.nc)
	}

	for i := range oldNotes {
		if consumedOld[i] {
			continue
		}
		n := oldNotes[i]
		nn := n
		changes = append(changes, MakeNoteChange(NoteChange{
			Kind: NoteRemoved, OldNote: &nn, NewNote: nil,
			HumanLabel: "Removed " + noteDescription(n, ppq),
		}))
	}
	for j := range newNotes {
		if consumedNew[j] {
			continue
		}
		n := newNotes[j]
		nn := n
		changes = append(changes, MakeNoteChange(NoteChange{
			Kind: NoteAdded, OldNote: nil, NewNote: &nn,
			HumanLabel: "Added " + noteDescription(n, ppq),
		}))
	}
	return changes
}

func buildNoteModified(oldNote, newNote NoteJson, ppq int) NoteChange {
	parts := []string{}
	if oldNote.Velocity != newNote.Velocity {
		parts = append(parts, fmt.Sprintf("velocity %d → %d", oldNote.Velocity, newNote.Velocity))
	}
	if oldNote.Length != newNote.Length {
		deltaTicks := int64(newNote.Length) - int64(oldNote.Length)
		label := fmt.Sprintf("length %d → %d ticks", oldNote.Length, newNote.Length)
		absDelta := deltaTicks
		if absDelta < 0 {
			absDelta = -absDelta
		}
		if absDelta >= int64(ppq/32) {
			musical := DescribePositionDelta(int(absDelta), ppq)
			suffix := "shorter"
			if deltaTicks > 0 {
				suffix = "longer"
			}
			stem := musical
			if i := strings.LastIndex(musical, " "); i != -1 {
				stem = musical[:i]
			}
			label = fmt.Sprintf("length %d → %d ticks (%s %s)", oldNote.Length, newNote.Length, stem, suffix)
		}
		parts = append(parts, label)
	}
	if oldNote.Pan != newNote.Pan {
		parts = append(parts, fmt.Sprintf("pan %d → %d", oldNote.Pan, newNote.Pan))
	}
	if oldNote.Release != newNote.Release {
		parts = append(parts, fmt.Sprintf("release %d → %d", oldNote.Release, newNote.Release))
	}
	if oldNote.FinePitch != newNote.FinePitch {
		parts = append(parts, fmt.Sprintf("fine pitch %d → %d", oldNote.FinePitch, newNote.FinePitch))
	}
	detail := "<unchanged>"
	if len(parts) > 0 {
		detail = strings.Join(parts, ", ")
	}
	on, nn := oldNote, newNote
	return MakeNoteChange(NoteChange{
		Kind: NoteModified, OldNote: &on, NewNote: &nn,
		HumanLabel: fmt.Sprintf("%s: %s", noteDescription(oldNote, ppq), detail),
	})
}

func buildNoteMoved(oldNote, newNote NoteJson, ppq int) NoteChange {
	delta := int(int64(newNote.Position) - int64(oldNote.Position))
	shift := DescribePositionDelta(delta, ppq)
	extras := []string{}
	if oldNote.Velocity != newNote.Velocity {
		extras = append(extras, fmt.Sprintf("velocity %d → %d", oldNote.Velocity, newNote.Velocity))
	}
	if oldNote.Length != newNote.Length {
		extras = append(extras, fmt.Sprintf("length %d → %d ticks", oldNote.Length, newNote.Length))
	}
	suffix := ""
	if len(extras) > 0 {
		suffix = " (" + strings.Join(extras, ", ") + ")"
	}
	on, nn := oldNote, newNote
	return MakeNoteChange(NoteChange{
		Kind: NoteMoved, OldNote: &on, NewNote: &nn,
		HumanLabel: fmt.Sprintf("%s on channel %d moved %s%s", NotePitchLabel(oldNote.Key), oldNote.ChannelIid, shift, suffix),
	})
}
