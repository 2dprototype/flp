package flpdiff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// FmtBeats renders ticks as a short beat-count string ("4", "4.5", "62.01").
func FmtBeats(ticks float64, ppq int) string {
	if ppq <= 0 {
		return jsNum(ticks) + "t"
	}
	beats := ticks / float64(ppq)
	if beats == float64(int64(beats)) {
		return jsNum(beats)
	}
	s := jsToFixed(beats, 3)
	// replace(/\.?0+$/, "")
	end := len(s)
	for end > 0 && s[end-1] == '0' {
		end--
	}
	if end > 0 && s[end-1] == '.' && end < len(s) {
		end--
	}
	if end == len(s) {
		return s
	}
	return s[:end]
}

func clipLocator(item PlaylistItemJson, ppq int) string {
	return "at beat " + FmtBeats(float64(item.Position), ppq)
}

func clipLengthSuffix(item PlaylistItemJson, ppq int) string {
	if item.Length == 0 {
		return ""
	}
	return fmt.Sprintf(", length %s beats", FmtBeats(float64(item.Length), ppq))
}

func clipRefKey(item PlaylistItemJson) int {
	if item.PatternIid != nil {
		return 10000000 + *item.PatternIid
	}
	if item.ChannelIid != nil {
		return *item.ChannelIid
	}
	return -1
}

func clipRefLabel(item PlaylistItemJson, channels map[int]*ChannelJson, patterns map[int]*PatternJson) string {
	if item.PatternIid != nil {
		name := fmt.Sprintf("#%d", *item.PatternIid)
		if p := patterns[*item.PatternIid]; p != nil && p.Name != nil {
			name = *p.Name
		}
		return fmt.Sprintf("pattern '%s'", name)
	}
	if item.ChannelIid != nil {
		if ch := channels[*item.ChannelIid]; ch != nil {
			name := fmt.Sprintf("#%d", *item.ChannelIid)
			if ch.Name != nil {
				name = *ch.Name
			}
			if ch.Kind == "automation" {
				return fmt.Sprintf("automation clip '%s'", name)
			}
			return fmt.Sprintf("clip '%s'", name)
		}
		return fmt.Sprintf("clip #%d", *item.ChannelIid)
	}
	return "empty clip"
}

func stripClipPrefix(ref string) string {
	if strings.HasPrefix(ref, "clip ") {
		return ref[5:]
	}
	return ref
}

func clipFullLabel(item PlaylistItemJson, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) string {
	ref := stripClipPrefix(clipRefLabel(item, channels, patterns))
	return fmt.Sprintf("%s %s%s", ref, clipLocator(item, ppq), clipLengthSuffix(item, ppq))
}

func mutedWord(muted bool) string {
	if muted {
		return "muted"
	}
	return "unmuted"
}

func buildClipModified(oldItem, newItem PlaylistItemJson, pathPrefix string, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) Change {
	parts := []string{}
	if oldItem.Length != newItem.Length {
		parts = append(parts, fmt.Sprintf("length %s → %s beats", FmtBeats(float64(oldItem.Length), ppq), FmtBeats(float64(newItem.Length), ppq)))
	}
	if oldItem.Muted != newItem.Muted {
		parts = append(parts, mutedWord(newItem.Muted))
	}
	detail := "<unchanged>"
	if len(parts) > 0 {
		detail = strings.Join(parts, ", ")
	}
	ref := clipRefLabel(oldItem, channels, patterns)
	return MakeChange(Change{
		Path: pathPrefix, Kind: KindModified, OldValue: oldItem, NewValue: newItem,
		HumanLabel: fmt.Sprintf("%s %s: %s", ref, clipLocator(oldItem, ppq), detail),
	})
}

func buildClipMoved(oldItem, newItem PlaylistItemJson, pathPrefix string, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) Change {
	ref := clipRefLabel(newItem, channels, patterns)
	shift := DescribePositionDelta(int(int64(newItem.Position)-int64(oldItem.Position)), ppq)
	extras := []string{}
	if oldItem.Length != newItem.Length {
		extras = append(extras, fmt.Sprintf("length %s → %s beats", FmtBeats(float64(oldItem.Length), ppq), FmtBeats(float64(newItem.Length), ppq)))
	}
	if oldItem.Muted != newItem.Muted {
		extras = append(extras, mutedWord(newItem.Muted))
	}
	suffix := ""
	if len(extras) > 0 {
		suffix = " (" + strings.Join(extras, ", ") + ")"
	}
	return MakeChange(Change{
		Path: pathPrefix, Kind: KindModified, OldValue: oldItem, NewValue: newItem,
		HumanLabel: fmt.Sprintf("%s moved from beat %s to beat %s (%s)%s", ref,
			FmtBeats(float64(oldItem.Position), ppq), FmtBeats(float64(newItem.Position), ppq), shift, suffix),
	})
}

func itemsEqual(a, b []PlaylistItemJson) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

type idxChange struct {
	idx int
	c   Change
}

func diffTrackItems(oldItems, newItems []PlaylistItemJson, trackIndex int, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) []Change {
	if itemsEqual(oldItems, newItems) {
		return []Change{}
	}
	exactKey := func(it PlaylistItemJson) string { return fmt.Sprintf("%d\x00%d", clipRefKey(it), it.Position) }

	oldByExact := map[string][]int{}
	for i, it := range oldItems {
		k := exactKey(it)
		oldByExact[k] = append(oldByExact[k], i)
	}
	consumedOld := map[int]bool{}
	consumedNew := map[int]bool{}
	exactMods := []idxChange{}

	for j, newItem := range newItems {
		bucket, ok := oldByExact[exactKey(newItem)]
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
		oldItem := oldItems[oldIdx]
		if reflect.DeepEqual(oldItem, newItem) {
			continue
		}
		path := fmt.Sprintf("tracks[%d].items[%d]", trackIndex, oldIdx)
		exactMods = append(exactMods, idxChange{oldIdx, buildClipModified(oldItem, newItem, path, channels, patterns, ppq)})
	}
	sort.SliceStable(exactMods, func(a, b int) bool { return exactMods[a].idx < exactMods[b].idx })
	changes := []Change{}
	for _, m := range exactMods {
		changes = append(changes, m.c)
	}

	oldByRef := map[int][]int{}
	for i, it := range oldItems {
		if consumedOld[i] {
			continue
		}
		k := clipRefKey(it)
		oldByRef[k] = append(oldByRef[k], i)
	}
	moves := []idxChange{}
	for j, newItem := range newItems {
		if consumedNew[j] {
			continue
		}
		bucket, ok := oldByRef[clipRefKey(newItem)]
		if !ok {
			continue
		}
		bestI := -1
		var bestDelta int64
		for _, i := range bucket {
			if consumedOld[i] {
				continue
			}
			d := int64(oldItems[i].Position) - int64(newItem.Position)
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
		path := fmt.Sprintf("tracks[%d].items[%d]", trackIndex, bestI)
		moves = append(moves, idxChange{bestI, buildClipMoved(oldItems[bestI], newItem, path, channels, patterns, ppq)})
	}
	sort.SliceStable(moves, func(a, b int) bool { return moves[a].idx < moves[b].idx })
	for _, m := range moves {
		changes = append(changes, m.c)
	}

	for i, item := range oldItems {
		if consumedOld[i] {
			continue
		}
		changes = append(changes, MakeChange(Change{
			Path: fmt.Sprintf("tracks[%d].items[%d]", trackIndex, i), Kind: KindRemoved, OldValue: item, NewValue: nil,
			HumanLabel: "Removed clip: " + clipFullLabel(item, channels, patterns, ppq),
		}))
	}
	for j, item := range newItems {
		if consumedNew[j] {
			continue
		}
		changes = append(changes, MakeChange(Change{
			Path: fmt.Sprintf("tracks[%d].items[%d]", trackIndex, j), Kind: KindAdded, OldValue: nil, NewValue: item,
			HumanLabel: "Added clip: " + clipFullLabel(item, channels, patterns, ppq),
		}))
	}
	return changes
}

// ───────────── clip-collapse groups ─────────────

const minClipGroupSize = 3

type clipMember struct {
	c    Change
	oldI PlaylistItemJson
	newI PlaylistItemJson
}

type clipBucket struct {
	delta   int
	kind    string
	length  int
	muted   bool
	oldLen  int
	newLen  int
	oldMut  bool
	newMut  bool
	members []clipMember
}

func addToBucket(order *[]string, buckets map[string]*clipBucket, key string, proto clipBucket, m clipMember) {
	b, ok := buckets[key]
	if !ok {
		p := proto
		b = &p
		buckets[key] = b
		*order = append(*order, key)
	}
	b.members = append(b.members, m)
}

func asClip(v interface{}) (PlaylistItemJson, bool) {
	if v == nil {
		return PlaylistItemJson{}, false
	}
	it, ok := v.(PlaylistItemJson)
	return it, ok
}

func posSummaryMove(positions []OldNewPos, ppq int) string {
	if len(positions) <= 3 {
		parts := make([]string, len(positions))
		for i, p := range positions {
			parts[i] = fmt.Sprintf("%s→%s", FmtBeats(float64(p[0]), ppq), FmtBeats(float64(p[1]), ppq))
		}
		return "beat " + strings.Join(parts, ", ")
	}
	first, last := positions[0], positions[len(positions)-1]
	return fmt.Sprintf("beats %s→%s … %s→%s",
		FmtBeats(float64(first[0]), ppq), FmtBeats(float64(first[1]), ppq),
		FmtBeats(float64(last[0]), ppq), FmtBeats(float64(last[1]), ppq))
}

func posSummaryList(positions []int, ppq int) string {
	if len(positions) <= 3 {
		parts := make([]string, len(positions))
		for i, p := range positions {
			parts[i] = FmtBeats(float64(p), ppq)
		}
		return "beats " + strings.Join(parts, ", ")
	}
	return fmt.Sprintf("beats %s … %s", FmtBeats(float64(positions[0]), ppq), FmtBeats(float64(positions[len(positions)-1]), ppq))
}

func buildClipMoveGroups(itemChanges []Change, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) []ClipMoveGroup {
	order := []string{}
	buckets := map[string]*clipBucket{}
	for _, c := range itemChanges {
		oldV, ok1 := asClip(c.OldValue)
		newV, ok2 := asClip(c.NewValue)
		if !ok1 || !ok2 {
			continue
		}
		if oldV.Length != newV.Length || oldV.Muted != newV.Muted {
			continue
		}
		if oldV.Position == newV.Position {
			continue
		}
		delta := int(int64(newV.Position) - int64(oldV.Position))
		key := fmt.Sprintf("%d\x00%d", clipRefKey(oldV), delta)
		addToBucket(&order, buckets, key, clipBucket{delta: delta}, clipMember{c, oldV, newV})
	}
	groups := []ClipMoveGroup{}
	for _, key := range order {
		b := buckets[key]
		members := b.members
		if len(members) < minClipGroupSize {
			continue
		}
		sort.SliceStable(members, func(a, c int) bool { return members[a].oldI.Position < members[c].oldI.Position })
		refLabel := clipRefLabel(members[0].oldI, channels, patterns)
		shiftDesc := DescribePositionDelta(b.delta, ppq)
		positions := make([]OldNewPos, len(members))
		changePaths := make([]string, len(members))
		for i, m := range members {
			positions[i] = OldNewPos{int(m.oldI.Position), int(m.newI.Position)}
			changePaths[i] = m.c.Path
		}
		posSummary := posSummaryMove(positions, ppq)
		humanLabel := fmt.Sprintf("%d clips of %s moved %s (%s)", len(members), stripClipPrefix(refLabel), shiftDesc, posSummary)
		groups = append(groups, MakeClipMoveGroup(ClipMoveGroup{
			RefLabel: refLabel, DeltaTicks: b.delta, Count: len(members), Positions: positions,
			ChangePaths: changePaths, HumanLabel: humanLabel,
		}))
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].Positions[0][0] < groups[b].Positions[0][0] })
	return groups
}

func buildClipBulkGroups(itemChanges []Change, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) []ClipBulkGroup {
	order := []string{}
	buckets := map[string]*clipBucket{}
	for _, c := range itemChanges {
		if c.Kind != KindAdded && c.Kind != KindRemoved {
			continue
		}
		var item PlaylistItemJson
		var ok bool
		if c.Kind == KindAdded {
			item, ok = asClip(c.NewValue)
		} else {
			item, ok = asClip(c.OldValue)
		}
		if !ok {
			continue
		}
		m := 0
		if item.Muted {
			m = 1
		}
		key := fmt.Sprintf("%s\x00%d\x00%d\x00%d", c.Kind, clipRefKey(item), item.Length, m)
		addToBucket(&order, buckets, key, clipBucket{kind: string(c.Kind), length: int(item.Length), muted: item.Muted}, clipMember{c: c, oldI: item, newI: item})
	}
	groups := []ClipBulkGroup{}
	for _, key := range order {
		b := buckets[key]
		members := b.members
		if len(members) < minClipGroupSize {
			continue
		}
		sort.SliceStable(members, func(a, c int) bool { return members[a].oldI.Position < members[c].oldI.Position })
		refLabel := clipRefLabel(members[0].oldI, channels, patterns)
		groupRef := stripClipPrefix(refLabel)
		positions := make([]int, len(members))
		changePaths := make([]string, len(members))
		for i, m := range members {
			positions[i] = int(m.oldI.Position)
			changePaths[i] = m.c.Path
		}
		lengthStr := FmtBeats(float64(b.length), ppq)
		verb := "removed"
		if b.kind == "added" {
			verb = "added"
		}
		posSummary := posSummaryList(positions, ppq)
		mutedSuffix := ""
		if b.muted {
			mutedSuffix = ", muted"
		}
		humanLabel := fmt.Sprintf("%d clips of %s %s (length %s beats%s, %s)", len(members), groupRef, verb, lengthStr, mutedSuffix, posSummary)
		groups = append(groups, MakeClipBulkGroup(ClipBulkGroup{
			Kind: b.kind, RefLabel: refLabel, LengthTicks: b.length, Muted: b.muted, Count: len(members),
			Positions: positions, ChangePaths: changePaths, HumanLabel: humanLabel,
		}))
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].Positions[0] < groups[b].Positions[0] })
	return groups
}

func buildClipModifyGroups(itemChanges []Change, channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) []ClipModifyGroup {
	order := []string{}
	buckets := map[string]*clipBucket{}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	for _, c := range itemChanges {
		if c.Kind != KindModified {
			continue
		}
		oldV, ok1 := asClip(c.OldValue)
		newV, ok2 := asClip(c.NewValue)
		if !ok1 || !ok2 {
			continue
		}
		if oldV.Position != newV.Position {
			continue
		}
		if oldV.Length == newV.Length && oldV.Muted == newV.Muted {
			continue
		}
		key := fmt.Sprintf("%d\x00%d\x00%d\x00%d\x00%d", clipRefKey(oldV), oldV.Length, newV.Length, b2i(oldV.Muted), b2i(newV.Muted))
		addToBucket(&order, buckets, key, clipBucket{oldLen: int(oldV.Length), newLen: int(newV.Length), oldMut: oldV.Muted, newMut: newV.Muted}, clipMember{c, oldV, newV})
	}
	groups := []ClipModifyGroup{}
	for _, key := range order {
		b := buckets[key]
		members := b.members
		if len(members) < minClipGroupSize {
			continue
		}
		sort.SliceStable(members, func(a, c int) bool { return members[a].oldI.Position < members[c].oldI.Position })
		refLabel := clipRefLabel(members[0].oldI, channels, patterns)
		groupRef := stripClipPrefix(refLabel)
		positions := make([]int, len(members))
		changePaths := make([]string, len(members))
		for i, m := range members {
			positions[i] = int(m.oldI.Position)
			changePaths[i] = m.c.Path
		}
		detailParts := []string{}
		if b.oldLen != b.newLen {
			detailParts = append(detailParts, fmt.Sprintf("length %s → %s beats", FmtBeats(float64(b.oldLen), ppq), FmtBeats(float64(b.newLen), ppq)))
		}
		if b.oldMut != b.newMut {
			detailParts = append(detailParts, mutedWord(b.newMut))
		}
		detail := strings.Join(detailParts, ", ")
		posSummary := posSummaryList(positions, ppq)
		humanLabel := fmt.Sprintf("%d clips of %s modified (%s, %s)", len(members), groupRef, detail, posSummary)
		groups = append(groups, MakeClipModifyGroup(ClipModifyGroup{
			RefLabel: refLabel, OldLengthTicks: b.oldLen, NewLengthTicks: b.newLen, OldMuted: b.oldMut, NewMuted: b.newMut,
			Count: len(members), Positions: positions, ChangePaths: changePaths, HumanLabel: humanLabel,
		}))
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].Positions[0] < groups[b].Positions[0] })
	return groups
}

// ───────────── tracks / arrangements ─────────────

func trackLabel(t TrackJson) string {
	name := fmt.Sprintf("#%d", t.Index)
	if t.Name != nil {
		name = *t.Name
	}
	return fmt.Sprintf("track '%s'", name)
}

// CompareTrack produces a TrackDiff for one pair.
func CompareTrack(match Match[TrackJson], channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) TrackDiff {
	if match.Old == nil && match.New != nil {
		return MakeTrackDiff(TrackDiff{
			Identity: []interface{}{"track", match.New.Index}, Kind: KindAdded, Index: match.New.Index,
			Name: match.New.Name, HumanLabel: "Added " + trackLabel(*match.New),
		})
	}
	if match.Old != nil && match.New == nil {
		return MakeTrackDiff(TrackDiff{
			Identity: []interface{}{"track", match.Old.Index}, Kind: KindRemoved, Index: match.Old.Index,
			Name: match.Old.Name, HumanLabel: "Removed " + trackLabel(*match.Old),
		})
	}
	oldT, newT := *match.Old, *match.New
	path := fmt.Sprintf("tracks[%d]", oldT.Index)
	changes := []Change{}

	if !ptrEq(oldT.Name, newT.Name) {
		pushChange(&changes, ScalarChange(path+".name", oldT.Name, newT.Name,
			fmt.Sprintf("Track renamed from %s to %s", FmtNoneFriendly(oldT.Name), FmtNoneFriendly(newT.Name))))
	}
	if !deepEqual(oldT.Color, newT.Color) {
		pushChange(&changes, ScalarChange(path+".color", oldT.Color, newT.Color,
			fmt.Sprintf("Track color: %s → %s", ColorHex(oldT.Color), ColorHex(newT.Color))))
	}
	if oldT.Height != newT.Height {
		pushChange(&changes, ScalarChange(path+".height", oldT.Height, newT.Height,
			fmt.Sprintf("Track height: %s → %s", FmtNoneFriendly(oldT.Height), FmtNoneFriendly(newT.Height))))
	}
	if oldT.Muted != newT.Muted {
		pushChange(&changes, ScalarChange(path+".muted", oldT.Muted, newT.Muted, "Track "+mutedWord(newT.Muted)))
	}

	itemChanges := diffTrackItems(oldT.Items, newT.Items, oldT.Index, channels, patterns, ppq)
	changes = append(changes, itemChanges...)

	moveGroups := buildClipMoveGroups(itemChanges, channels, patterns, ppq)
	bulkGroups := buildClipBulkGroups(itemChanges, channels, patterns, ppq)
	modifyGroups := buildClipModifyGroups(itemChanges, channels, patterns, ppq)

	var label string
	if len(changes) > 0 {
		label = fmt.Sprintf("%s modified (%d changes)", capitalize(trackLabel(oldT)), len(changes))
	} else {
		label = fmt.Sprintf("%s unchanged", capitalize(trackLabel(oldT)))
	}
	return MakeTrackDiff(TrackDiff{
		Identity: []interface{}{"track", oldT.Index}, Kind: KindModified, Index: oldT.Index, Name: oldT.Name,
		HumanLabel: label, Changes: changes, ClipMoveGroups: moveGroups, ClipBulkGroups: bulkGroups, ClipModifyGroups: modifyGroups,
	})
}

func arrangementLabel(a ArrangementJson) string {
	name := fmt.Sprintf("#%d", a.Index)
	if a.Name != nil {
		name = *a.Name
	}
	return fmt.Sprintf("arrangement '%s'", name)
}

// CompareArrangement produces an ArrangementDiff for one pair.
func CompareArrangement(match Match[ArrangementJson], channels map[int]*ChannelJson, patterns map[int]*PatternJson, ppq int) ArrangementDiff {
	if match.Old == nil && match.New != nil {
		return MakeArrangementDiff(ArrangementDiff{
			Identity: []interface{}{"arrangement", match.New.Index}, Kind: KindAdded, Name: match.New.Name,
			HumanLabel: "Added " + arrangementLabel(*match.New),
		})
	}
	if match.Old != nil && match.New == nil {
		return MakeArrangementDiff(ArrangementDiff{
			Identity: []interface{}{"arrangement", match.Old.Index}, Kind: KindRemoved, Name: match.Old.Name,
			HumanLabel: "Removed " + arrangementLabel(*match.Old),
		})
	}
	oldA, newA := *match.Old, *match.New
	changes := []Change{}
	if !ptrEq(oldA.Name, newA.Name) {
		pushChange(&changes, ScalarChange(fmt.Sprintf("arrangements[%d].name", oldA.Index), oldA.Name, newA.Name,
			fmt.Sprintf("Arrangement renamed from %s to %s", FmtNoneFriendly(oldA.Name), FmtNoneFriendly(newA.Name))))
	}

	trackMatches := pairByKey(oldA.Tracks, newA.Tracks,
		func(t *TrackJson) string { return fmt.Sprint(t.Index) },
		func(t *TrackJson) (string, bool) { return optName(t.Name) })
	trackChanges := []TrackDiff{}
	for _, m := range trackMatches {
		td := CompareTrack(m, channels, patterns, ppq)
		if td.Kind == KindAdded || td.Kind == KindRemoved || len(td.Changes) > 0 {
			trackChanges = append(trackChanges, td)
		}
	}

	var label string
	if len(changes) > 0 || len(trackChanges) > 0 {
		label = fmt.Sprintf("%s modified (%d arrangement changes, %d track changes)", capitalize(arrangementLabel(oldA)), len(changes), len(trackChanges))
	} else {
		label = fmt.Sprintf("%s unchanged", capitalize(arrangementLabel(oldA)))
	}
	return MakeArrangementDiff(ArrangementDiff{
		Identity: []interface{}{"arrangement", oldA.Index}, Kind: KindModified, Name: oldA.Name,
		HumanLabel: label, Changes: changes, TrackChanges: trackChanges,
	})
}
