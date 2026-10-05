package flpdiff

import (
	"fmt"
	"sort"
	"strings"
)

func describeAutomationPosition(position float64, ppq int) string {
	beat := 0.0
	if ppq != 0 {
		beat = position / float64(ppq)
	}
	return "beat " + PythonGFormat(beat)
}

func buildAutoModified(oldPt, newPt AutomationPointJson, ppq int) AutomationChange {
	parts := []string{}
	if oldPt.Value != newPt.Value {
		parts = append(parts, fmt.Sprintf("value %s → %s", PythonGFormat(oldPt.Value), PythonGFormat(newPt.Value)))
	}
	if oldPt.Tension != newPt.Tension {
		parts = append(parts, fmt.Sprintf("tension %s → %s", PythonGFormat(oldPt.Tension), PythonGFormat(newPt.Tension)))
	}
	detail := "<unchanged>"
	if len(parts) > 0 {
		detail = strings.Join(parts, ", ")
	}
	o, n := oldPt, newPt
	return MakeAutomationChange(AutomationChange{
		Kind: KindModified, OldPoint: &o, NewPoint: &n,
		HumanLabel: fmt.Sprintf("Keyframe at %s: %s", describeAutomationPosition(oldPt.Position, ppq), detail),
	})
}

func buildAutoAdded(newPt AutomationPointJson, ppq int) AutomationChange {
	n := newPt
	return MakeAutomationChange(AutomationChange{
		Kind: KindAdded, OldPoint: nil, NewPoint: &n,
		HumanLabel: fmt.Sprintf("Added keyframe at %s (value %s)", describeAutomationPosition(newPt.Position, ppq), PythonGFormat(newPt.Value)),
	})
}

func buildAutoRemoved(oldPt AutomationPointJson, ppq int) AutomationChange {
	o := oldPt
	return MakeAutomationChange(AutomationChange{
		Kind: KindRemoved, OldPoint: &o, NewPoint: nil,
		HumanLabel: fmt.Sprintf("Removed keyframe at %s (value %s)", describeAutomationPosition(oldPt.Position, ppq), PythonGFormat(oldPt.Value)),
	})
}

// DiffAutomationPoints produces a timeline-ordered, position-anchored keyframe diff.
func DiffAutomationPoints(oldPts, newPts []AutomationPointJson, ppq int) []AutomationChange {
	oldByPos := map[float64][]int{}
	for i, p := range oldPts {
		oldByPos[p.Position] = append(oldByPos[p.Position], i)
	}
	consumedOld := map[int]bool{}
	consumedNew := map[int]bool{}
	modifieds := []AutomationChange{}

	for j, kf := range newPts {
		bucket, ok := oldByPos[kf.Position]
		if !ok {
			continue
		}
		matchIdx := -1
		for _, i := range bucket {
			if !consumedOld[i] {
				matchIdx = i
				break
			}
		}
		if matchIdx < 0 {
			continue
		}
		consumedOld[matchIdx] = true
		consumedNew[j] = true
		oldKf := oldPts[matchIdx]
		if oldKf.Value == kf.Value && oldKf.Tension == kf.Tension {
			continue
		}
		modifieds = append(modifieds, buildAutoModified(oldKf, kf, ppq))
	}
	removeds := []AutomationChange{}
	for i := range oldPts {
		if !consumedOld[i] {
			removeds = append(removeds, buildAutoRemoved(oldPts[i], ppq))
		}
	}
	addeds := []AutomationChange{}
	for j := range newPts {
		if !consumedNew[j] {
			addeds = append(addeds, buildAutoAdded(newPts[j], ppq))
		}
	}
	rank := map[ChangeKind]int{KindModified: 0, KindRemoved: 1, KindAdded: 2}
	out := make([]AutomationChange, 0, len(modifieds)+len(removeds)+len(addeds))
	out = append(out, modifieds...)
	out = append(out, removeds...)
	out = append(out, addeds...)
	pos := func(c AutomationChange) float64 {
		if c.OldPoint != nil {
			return c.OldPoint.Position
		}
		return c.NewPoint.Position
	}
	sort.SliceStable(out, func(a, b int) bool {
		ap, bp := pos(out[a]), pos(out[b])
		if ap != bp {
			return ap < bp
		}
		return rank[out[a].Kind] < rank[out[b].Kind]
	})
	return out
}
