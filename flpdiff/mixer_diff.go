package flpdiff

import (
	"fmt"
	"strings"
)

func insertLabel(ins MixerInsertJson) string {
	name := "unnamed"
	if ins.Name != nil {
		name = *ins.Name
	}
	if ins.Index == 0 {
		return fmt.Sprintf("Master (%s)", name)
	}
	return fmt.Sprintf("Insert %d (%s)", ins.Index, name)
}

// CompareSlots diffs slots, index-matched.
func CompareSlots(pathPrefix string, oldSlots, newSlots []MixerSlotJson) []Change {
	out := []Change{}
	maxLen := len(oldSlots)
	if len(newSlots) > maxLen {
		maxLen = len(newSlots)
	}
	for i := 0; i < maxLen; i++ {
		var oldS, newS *MixerSlotJson
		if i < len(oldSlots) {
			oldS = &oldSlots[i]
		}
		if i < len(newSlots) {
			newS = &newSlots[i]
		}
		if deepEqual(oldS, newS) {
			continue
		}
		slotPath := fmt.Sprintf("%s[%d]", pathPrefix, i)
		if oldS == nil || newS == nil {
			word := "removed"
			if oldS == nil {
				word = "added"
			}
			var ov, nv interface{}
			if oldS != nil {
				ov = *oldS
			}
			if newS != nil {
				nv = *newS
			}
			out = append(out, MakeChange(Change{
				Path: slotPath, Kind: Classify(ov, nv), OldValue: ov, NewValue: nv,
				HumanLabel: fmt.Sprintf("Slot %d %s", i, word),
			}))
			continue
		}
		if oldS.Enabled != newS.Enabled {
			w := "bypassed"
			if newS.Enabled {
				w = "enabled"
			}
			out = append(out, MakeChange(Change{
				Path: slotPath + ".enabled", Kind: KindModified, OldValue: oldS.Enabled, NewValue: newS.Enabled,
				HumanLabel: fmt.Sprintf("Slot %d %s", i, w),
			}))
		}
		out = append(out, ComparePluginLabels(slotPath+".plugin", oldS.Plugin, newS.Plugin, fmt.Sprintf(" in slot %d", i))...)
	}
	return out
}

func pythonIntListRepr(arr []int) string {
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = fmt.Sprint(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// CompareMixerInsert produces a MixerInsertDiff for one pair.
func CompareMixerInsert(match Match[MixerInsertJson]) MixerInsertDiff {
	if match.Old == nil && match.New != nil {
		return MakeMixerInsertDiff(MixerInsertDiff{
			Identity: []interface{}{"insert", match.New.Index}, Kind: KindAdded, Index: match.New.Index,
			Name: match.New.Name, HumanLabel: "Added " + insertLabel(*match.New),
		})
	}
	if match.Old != nil && match.New == nil {
		return MakeMixerInsertDiff(MixerInsertDiff{
			Identity: []interface{}{"insert", match.Old.Index}, Kind: KindRemoved, Index: match.Old.Index,
			Name: match.Old.Name, HumanLabel: "Removed " + insertLabel(*match.Old),
		})
	}
	oldIns, newIns := *match.Old, *match.New
	path := fmt.Sprintf("mixer.inserts[%d]", oldIns.Index)
	changes := []Change{}

	if !ptrEq(oldIns.Name, newIns.Name) {
		pushChange(&changes, ScalarChange(path+".name", oldIns.Name, newIns.Name,
			fmt.Sprintf("Insert renamed from %s to %s", FmtNoneFriendly(oldIns.Name), FmtNoneFriendly(newIns.Name))))
	}
	if !deepEqual(oldIns.Color, newIns.Color) {
		pushChange(&changes, ScalarChange(path+".color", oldIns.Color, newIns.Color,
			fmt.Sprintf("Insert color: %s → %s", ColorHex(oldIns.Color), ColorHex(newIns.Color))))
	}
	if oldIns.Enabled != newIns.Enabled {
		pushChange(&changes, ScalarChange(path+".enabled", oldIns.Enabled, newIns.Enabled,
			fmt.Sprintf("Insert %s (was %s)", FmtBool(newIns.Enabled), FmtBool(oldIns.Enabled))))
	}
	if oldIns.Locked != newIns.Locked {
		w := "unlocked"
		if newIns.Locked {
			w = "locked"
		}
		pushChange(&changes, ScalarChange(path+".locked", oldIns.Locked, newIns.Locked, "Insert "+w))
	}
	if !ptrEq(oldIns.Volume, newIns.Volume) {
		pushChange(&changes, ScalarChange(path+".volume", oldIns.Volume, newIns.Volume,
			fmt.Sprintf("Insert volume %s → %s", FmtPct(oldIns.Volume), FmtPct(newIns.Volume))))
	}
	if !ptrEq(oldIns.Pan, newIns.Pan) {
		pushChange(&changes, ScalarChange(path+".pan", oldIns.Pan, newIns.Pan,
			fmt.Sprintf("Insert pan %s → %s", FmtPan(oldIns.Pan), FmtPan(newIns.Pan))))
	}
	if !ptrEq(oldIns.StereoSeparation, newIns.StereoSeparation) {
		pushChange(&changes, ScalarChange(path+".stereo_separation", oldIns.StereoSeparation, newIns.StereoSeparation,
			fmt.Sprintf("Stereo separation %s → %s", FmtPct(oldIns.StereoSeparation), FmtPct(newIns.StereoSeparation))))
	}
	if !deepEqual(oldIns.RoutesTo, newIns.RoutesTo) {
		changes = append(changes, MakeChange(Change{
			Path: path + ".routes_to", Kind: KindModified, OldValue: oldIns.RoutesTo, NewValue: newIns.RoutesTo,
			HumanLabel: fmt.Sprintf("Insert routing changed: %s → %s", pythonIntListRepr(oldIns.RoutesTo), pythonIntListRepr(newIns.RoutesTo)),
		}))
	}

	changes = append(changes, CompareSlots(path+".slots", oldIns.Slots, newIns.Slots)...)

	var label string
	if len(changes) > 0 {
		label = fmt.Sprintf("%s modified (%d changes)", insertLabel(oldIns), len(changes))
	} else {
		label = fmt.Sprintf("%s unchanged", insertLabel(oldIns))
	}
	return MakeMixerInsertDiff(MixerInsertDiff{
		Identity: []interface{}{"insert", oldIns.Index}, Kind: KindModified, Index: oldIns.Index,
		Name: oldIns.Name, HumanLabel: label, Changes: changes,
	})
}

// CompareMixer aggregates per-insert diffs into a MixerDiff.
func CompareMixer(matches []Match[MixerInsertJson]) MixerDiff {
	inserts := []MixerInsertDiff{}
	for _, m := range matches {
		d := CompareMixerInsert(m)
		if d.Kind == KindAdded || d.Kind == KindRemoved || len(d.Changes) > 0 {
			inserts = append(inserts, d)
		}
	}
	return MakeMixerDiff(MixerDiff{Inserts: inserts})
}

// CompareMixerFromJson matches inserts at the JSON level then compares.
func CompareMixerFromJson(oldInserts, newInserts []MixerInsertJson) MixerDiff {
	matches := pairByKey(oldInserts, newInserts,
		func(i *MixerInsertJson) string { return fmt.Sprint(i.Index) },
		func(i *MixerInsertJson) (string, bool) { return optName(i.Name) })
	return CompareMixer(matches)
}
