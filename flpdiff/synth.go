package flpdiff

import (
	"fmt"
)

// ───────────── craft-plugin-fixture ─────────────

func decodeNameStrict(payload []byte) string { return decodeUtf16LeNullTerminatedStrict(payload) }

// PluginSlotScope is a slice of events extracted from a donor FLP.
type PluginSlotScope struct {
	Scope       []FLPEvent
	DonorInsert int
	DonorSlot   int
}

// ExtractPluginSlotScope extracts the events of the first mixer slot hosting pluginName.
func ExtractPluginSlotScope(donor *FLPProject, pluginName string) (*PluginSlotScope, error) {
	curInsert, curSlot := 0, 0
	inMixer := false
	candidateStart := -1
	var candidateName string
	hasCandidate := false
	for i, ev := range donor.Events {
		if ev.Opcode == opInsertFlags {
			inMixer = true
			curSlot = 0
			candidateStart = i + 1
			candidateName, hasCandidate = "", false
			continue
		}
		if !inMixer {
			continue
		}
		if ev.Kind == KindU16 && ev.Opcode == opNewSlot {
			if candidateStart >= 0 && hasCandidate && candidateName == pluginName {
				return &PluginSlotScope{Scope: append([]FLPEvent(nil), donor.Events[candidateStart:i]...), DonorInsert: curInsert, DonorSlot: curSlot}, nil
			}
			curSlot = int(ev.Value)
			candidateStart = i + 1
			candidateName, hasCandidate = "", false
			continue
		}
		if ev.Opcode == opInsertEnd {
			if candidateStart >= 0 && hasCandidate && candidateName == pluginName {
				return &PluginSlotScope{Scope: append([]FLPEvent(nil), donor.Events[candidateStart:i]...), DonorInsert: curInsert, DonorSlot: curSlot}, nil
			}
			curInsert++
			curSlot = 0
			candidateStart = -1
			candidateName, hasCandidate = "", false
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginInternal {
			if n := decodeNameStrict(ev.Payload); len(n) > 0 && !hasCandidate {
				candidateName, hasCandidate = n, true
			}
		}
		if ev.Kind == KindBlob && ev.Opcode == opName {
			if n := decodeNameStrict(ev.Payload); len(n) > 0 {
				candidateName, hasCandidate = n, true
			}
		}
	}
	return nil, fmt.Errorf("plugin \"%s\" not found in donor FLP", pluginName)
}

// CraftPluginFixture splices scope into baseline after the target insert/slot marker.
func CraftPluginFixture(baseline *FLPProject, scope []FLPEvent, targetInsert, targetSlotMarker int) (*FLPProject, error) {
	events := cloneEvents(baseline)
	curInsert := 0
	inMixer := false
	insertedAt := -1
	for i, ev := range events {
		if ev.Opcode == opInsertFlags {
			inMixer = true
			continue
		}
		if !inMixer {
			continue
		}
		if ev.Opcode == opInsertEnd {
			curInsert++
			continue
		}
		if curInsert == targetInsert && ev.Kind == KindU16 && ev.Opcode == opNewSlot {
			if int(ev.Value) == targetSlotMarker {
				insertedAt = i + 1
				break
			}
		}
	}
	if insertedAt < 0 {
		return nil, fmt.Errorf("target insert %d slot marker %d not found in baseline", targetInsert, targetSlotMarker)
	}
	events = insertEvents(events, insertedAt, scope...)
	return withEvents(baseline, events), nil
}

// ───────────── extract-fst ─────────────

// FstContainerKind is the FLhd format value of .fst preset files.
const FstContainerKind = 0x0030

const opFlVersionBanner = 0xc7

// FstParseError is returned when a donor is not a valid .fst preset.
type FstParseError struct{ Message string }

func (e *FstParseError) Error() string { return e.Message }

func looksUtf16Le(p []byte) bool {
	if len(p) < 2 || len(p)%2 != 0 {
		return false
	}
	zeros, total := 0, 0
	for i := 1; i < len(p); i += 2 {
		total++
		if p[i] == 0 {
			zeros++
		}
	}
	return total > 0 && float64(zeros)/float64(total) > 0.5
}

func decodeFstName(p []byte) string {
	end := len(p)
	if looksUtf16Le(p) {
		for end >= 2 && p[end-1] == 0 && p[end-2] == 0 {
			end -= 2
		}
		return utf16LEToString(p[:end])
	}
	for end >= 1 && p[end-1] == 0 {
		end--
	}
	runes := make([]rune, end)
	for i := 0; i < end; i++ {
		runes[i] = rune(p[i]) // latin1
	}
	return string(runes)
}

// ExtractedPluginScope is the result of extracting a plugin scope from an .fst.
type ExtractedPluginScope struct {
	Scope              []FLPEvent
	PluginInternalName string
	PluginDisplayName  *string
}

// ExtractPluginScopeFromFst extracts the plugin scope from a parsed .fst donor.
func ExtractPluginScopeFromFst(donor *FLPProject) (*ExtractedPluginScope, error) {
	if donor.Header.Format != FstContainerKind {
		return nil, &FstParseError{fmt.Sprintf("expected .fst container kind 0x%04x, got 0x%04x", FstContainerKind, donor.Header.Format)}
	}
	var internalName, displayName *string
	scope := []FLPEvent{}
	for _, ev := range donor.Events {
		if ev.Opcode == opFlVersionBanner {
			continue
		}
		if ev.Kind == KindBlob && ev.Opcode == opPluginInternal && internalName == nil {
			internalName = Ptr(decodeFstName(ev.Payload))
		}
		if ev.Kind == KindBlob && ev.Opcode == opName && displayName == nil {
			displayName = Ptr(decodeFstName(ev.Payload))
		}
		scope = append(scope, ev)
	}
	if internalName == nil {
		return nil, &FstParseError{"no 0xC9 plugin-internal-name event in .fst donor — not a valid native plugin preset"}
	}
	return &ExtractedPluginScope{Scope: scope, PluginInternalName: *internalName, PluginDisplayName: displayName}, nil
}
