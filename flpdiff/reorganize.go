package flpdiff

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// GroupKey identifies an instrument family.
type GroupKey string

const (
	GroupDrumsHard GroupKey = "drums_hard"
	GroupDrumsSoft GroupKey = "drums_soft"
	GroupBass      GroupKey = "bass"
	GroupLead      GroupKey = "lead"
	GroupPad       GroupKey = "pad"
	GroupFx        GroupKey = "fx"
	GroupVocal     GroupKey = "vocal"
	GroupOther     GroupKey = "other"
)

// RGB is a plain colour triple used by the reorganiser.
type RGB struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// Group is a classification result.
type Group struct {
	Key  GroupKey
	Name string
	RGB  RGB
}

var (
	paletteDrumsHard = RGB{233, 75, 60}
	paletteDrumsSoft = RGB{255, 140, 66}
	paletteBass      = RGB{59, 130, 246}
	paletteLead      = RGB{34, 197, 94}
	palettePad       = RGB{139, 92, 246}
	paletteFx        = RGB{236, 72, 153}
	paletteVocal     = RGB{250, 204, 21}
	paletteOther     = RGB{100, 116, 139}
)

type groupEntry struct {
	Keyword string
	Group   Group
}

// GroupEntries is the ordered keyword → group table (first match wins).
var GroupEntries = []groupEntry{
	{"kick", Group{GroupDrumsHard, "Kick", paletteDrumsHard}},
	{"snare", Group{GroupDrumsHard, "Snare", paletteDrumsHard}},
	{"clap", Group{GroupDrumsHard, "Clap", paletteDrumsHard}},
	{"rim", Group{GroupDrumsHard, "Rim", paletteDrumsHard}},
	{"hat", Group{GroupDrumsSoft, "HiHat", paletteDrumsSoft}},
	{"perc", Group{GroupDrumsSoft, "Perc", paletteDrumsSoft}},
	{"tom", Group{GroupDrumsSoft, "Tom", paletteDrumsSoft}},
	{"shaker", Group{GroupDrumsSoft, "Shaker", paletteDrumsSoft}},
	{"cymbal", Group{GroupDrumsSoft, "Cymbal", paletteDrumsSoft}},
	{"crash", Group{GroupDrumsSoft, "Crash", paletteDrumsSoft}},
	{"ride", Group{GroupDrumsSoft, "Ride", paletteDrumsSoft}},
	{"drum", Group{GroupDrumsHard, "Drum", paletteDrumsHard}},
	{"vox", Group{GroupVocal, "Vocal", paletteVocal}},
	{"vocal", Group{GroupVocal, "Vocal", paletteVocal}},
	{"bass", Group{GroupBass, "Bass", paletteBass}},
	{"sub", Group{GroupBass, "Sub Bass", paletteBass}},
	{"808", Group{GroupBass, "808", paletteBass}},
	{"lead", Group{GroupLead, "Lead", paletteLead}},
	{"melody", Group{GroupLead, "Melody", paletteLead}},
	{"arp", Group{GroupLead, "Arp", paletteLead}},
	{"synth", Group{GroupLead, "Synth", paletteLead}},
	{"piano", Group{GroupLead, "Piano", paletteLead}},
	{"pad", Group{GroupPad, "Pad", palettePad}},
	{"string", Group{GroupPad, "Strings", palettePad}},
	{"atmos", Group{GroupPad, "Atmos", palettePad}},
	{"fx", Group{GroupFx, "FX", paletteFx}},
	{"riser", Group{GroupFx, "Riser", paletteFx}},
	{"sweep", Group{GroupFx, "Sweep", paletteFx}},
	{"impact", Group{GroupFx, "Impact", paletteFx}},
}

// FamilyOrder is the vertical order of families in the playlist.
var FamilyOrder = []GroupKey{GroupDrumsHard, GroupDrumsSoft, GroupBass, GroupLead, GroupPad, GroupFx, GroupVocal, GroupOther}

type familyLabel struct {
	Label string
	RGB   RGB
}

// FamilyLabels maps a family to its separator label and colour.
var FamilyLabels = map[GroupKey]familyLabel{
	GroupDrumsHard: {"Drums", paletteDrumsHard},
	GroupDrumsSoft: {"Drums (Soft)", paletteDrumsSoft},
	GroupBass:      {"Bass", paletteBass},
	GroupLead:      {"Synth", paletteLead},
	GroupPad:       {"Pad", palettePad},
	GroupFx:        {"FX", paletteFx},
	GroupVocal:     {"Vocal", paletteVocal},
	GroupOther:     {"Other", paletteOther},
}

var otherGroup = Group{GroupOther, "Other", paletteOther}

var keywordRegexes = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(GroupEntries))
	for i, e := range GroupEntries {
		out[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(e.Keyword) + `\b`)
	}
	return out
}()

var camelRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func splitCamelCase(text string) string { return camelRe.ReplaceAllString(text, "$1 $2") }

// ClassifyChannel returns the first keyword group matching the channel name/sample path.
func ClassifyChannel(name, samplePath *string) *Group {
	raw := strOr(name, "") + " " + strOr(samplePath, "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	text := splitCamelCase(raw)
	for i, re := range keywordRegexes {
		if re.MatchString(text) {
			g := GroupEntries[i].Group
			return &g
		}
	}
	return nil
}

func groupByKeyword(kw string) *Group {
	for _, e := range GroupEntries {
		if e.Keyword == kw {
			g := e.Group
			return &g
		}
	}
	return nil
}

// ClassifyByPitchRange classifies by mean pitch (<48 → bass, else lead).
func ClassifyByPitchRange(notes []Note) *Group {
	if len(notes) == 0 {
		return nil
	}
	sum := 0.0
	for _, n := range notes {
		sum += float64(n.Key)
	}
	if sum/float64(len(notes)) < 48 {
		return groupByKeyword("bass")
	}
	return groupByKeyword("lead")
}

// TrackMutation is one planned track edit.
type TrackMutation struct {
	TrackIndex        int    `json:"trackIndex"`
	Name              string `json:"name"`
	RGB               RGB    `json:"rgb"`
	Grouped           bool   `json:"grouped"`
	IsFamilySeparator bool   `json:"isFamilySeparator,omitempty"`
}

// ClipMoveMutation is one planned clip move.
type ClipMoveMutation struct {
	FromTrackIndex    int    `json:"fromTrackIndex"`
	FromPositionTicks uint32 `json:"fromPositionTicks"`
	RefID             int    `json:"refId"`
	RefKind           string `json:"refKind"`
	ToTrackIndex      int    `json:"toTrackIndex"`
}

// ReorganizePlan is the dry-run output of PlanReorganize.
type ReorganizePlan struct {
	ArrangementID int                `json:"arrangementId"`
	Tracks        []TrackMutation    `json:"tracks"`
	ClipMoves     []ClipMoveMutation `json:"clipMoves"`
}

// ReorganizeOptions configures planning. Nil fields take defaults (arrangement 0, separators on).
type ReorganizeOptions struct {
	ArrangementID       *int
	AddFamilySeparators *bool
}

type lane struct {
	kind                string
	refID               int
	group               Group
	displayName         string
	sortKey             int
	clipIndices         []int
	isAutomation        bool
	automationTargetIid *int
}

func notesByChannelIid(p *FLPProject) map[int][]Note {
	out := map[int][]Note{}
	for _, pat := range p.Patterns {
		for _, n := range pat.Notes {
			out[n.ChannelIid] = append(out[n.ChannelIid], n)
		}
	}
	return out
}

func dominantChannelIid(notes []Note) *int {
	if len(notes) == 0 {
		return nil
	}
	counts := map[int]int{}
	order := []int{}
	for _, n := range notes {
		if _, ok := counts[n.ChannelIid]; !ok {
			order = append(order, n.ChannelIid)
		}
		counts[n.ChannelIid]++
	}
	bestIid, bestCount := -1, -1
	for _, iid := range order {
		if counts[iid] > bestCount {
			bestCount = counts[iid]
			bestIid = iid
		}
	}
	if bestIid >= 0 {
		return &bestIid
	}
	return nil
}

func trimOr(p *string, fallback string) string {
	if p != nil {
		if t := strings.TrimSpace(*p); t != "" {
			return t
		}
	}
	return fallback
}

type orderedLanes struct {
	keys []string
	m    map[string]*lane
}

func collectLanes(p *FLPProject, arrangementID int) *orderedLanes {
	ol := &orderedLanes{m: map[string]*lane{}}
	var arr *Arrangement
	for i := range p.Arrangements {
		if p.Arrangements[i].ID == arrangementID {
			arr = &p.Arrangements[i]
			break
		}
	}
	if arr == nil {
		return ol
	}
	channelsByIid := map[int]*Channel{}
	for i := range p.Channels {
		channelsByIid[p.Channels[i].Iid] = &p.Channels[i]
	}
	patternsByID := map[int]*Pattern{}
	for i := range p.Patterns {
		patternsByID[p.Patterns[i].ID] = &p.Patterns[i]
	}
	allNotes := notesByChannelIid(p)
	clipCountByCh := map[int]int{}
	for _, clip := range arr.Clips {
		if clip.ItemIndex > playlistPatternBase {
			continue
		}
		clipCountByCh[clip.ItemIndex]++
	}
	type cc struct{ iid, clipCount int }
	channelClipCounts := map[int][]cc{}
	for _, ch := range p.Channels {
		if ch.TargetInsert == nil || *ch.TargetInsert < 0 {
			continue
		}
		channelClipCounts[*ch.TargetInsert] = append(channelClipCounts[*ch.TargetInsert], cc{ch.Iid, clipCountByCh[ch.Iid]})
	}

	for i, clip := range arr.Clips {
		isPattern := clip.ItemIndex > playlistPatternBase
		kind := "channel"
		refID := clip.ItemIndex
		if isPattern {
			kind = "pattern"
			refID = clip.ItemIndex - playlistPatternBase
		}
		key := fmt.Sprintf("%s:%d", kind, refID)
		ln := ol.m[key]
		if ln == nil {
			var group *Group
			displayName := ""
			sortKey := refID
			isAutomation := false
			var autoTarget *int
			if kind == "channel" {
				ch := channelsByIid[refID]
				chNotes := allNotes[refID]
				var chName, chSample *string
				if ch != nil {
					chName, chSample = ch.Name, ch.SamplePath
				}
				group = ClassifyChannel(chName, chSample)
				if group == nil {
					group = ClassifyByPitchRange(chNotes)
				}
				displayName = trimOr(chName, fmt.Sprintf("Channel %d", refID))
				if ch != nil && ch.Kind == ChannelAutomation && ch.AutomationTarget != nil &&
					ch.AutomationTarget.Kind == "channel" && ch.AutomationTarget.TargetChannelIid != nil {
					isAutomation = true
					t := *ch.AutomationTarget.TargetChannelIid
					autoTarget = &t
					if target := channelsByIid[t]; target != nil {
						tg := ClassifyChannel(target.Name, target.SamplePath)
						if tg == nil {
							tg = ClassifyByPitchRange(allNotes[target.Iid])
						}
						if tg != nil {
							group = tg
						}
					}
				} else if ch != nil && ch.Kind == ChannelAutomation && ch.AutomationTarget != nil &&
					ch.AutomationTarget.Kind == "mixer_slot" && ch.AutomationTarget.TargetInsertIndex != nil {
					isAutomation = true
					candidates := channelClipCounts[*ch.AutomationTarget.TargetInsertIndex]
					if len(candidates) > 0 {
						sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].clipCount > candidates[b].clipCount })
						t := candidates[0].iid
						autoTarget = &t
						if target := channelsByIid[t]; target != nil {
							tg := ClassifyChannel(target.Name, target.SamplePath)
							if tg == nil {
								tg = ClassifyByPitchRange(allNotes[target.Iid])
							}
							if tg != nil {
								group = tg
							}
						}
					}
				} else if ch != nil && ch.Kind == ChannelAutomation {
					isAutomation = true
				}
			} else {
				pat := patternsByID[refID]
				var patNotes []Note
				var patName *string
				if pat != nil {
					patNotes, patName = pat.Notes, pat.Name
				}
				dominant := dominantChannelIid(patNotes)
				var dominantCh *Channel
				if dominant != nil {
					dominantCh = channelsByIid[*dominant]
				}
				var dName, dSample *string
				if dominantCh != nil {
					dName, dSample = dominantCh.Name, dominantCh.SamplePath
				}
				group = ClassifyChannel(dName, dSample)
				if group == nil {
					group = ClassifyByPitchRange(patNotes)
				}
				displayName = trimOr(patName, trimOr(dName, fmt.Sprintf("Pattern %d", refID)))
				if dominant != nil {
					sortKey = *dominant
				} else {
					sortKey = refID + 100000
				}
			}
			if group == nil {
				g := otherGroup
				group = &g
			}
			ln = &lane{kind: kind, refID: refID, group: *group, displayName: displayName, sortKey: sortKey,
				isAutomation: isAutomation, automationTargetIid: autoTarget}
			ol.m[key] = ln
			ol.keys = append(ol.keys, key)
		}
		ln.clipIndices = append(ln.clipIndices, i)
	}
	return ol
}

// PlanReorganize computes the reorganisation plan without mutating the project.
func PlanReorganize(p *FLPProject, opts ReorganizeOptions) ReorganizePlan {
	arrangementID := 0
	if opts.ArrangementID != nil {
		arrangementID = *opts.ArrangementID
	}
	addSeparators := true
	if opts.AddFamilySeparators != nil {
		addSeparators = *opts.AddFamilySeparators
	}
	var arr *Arrangement
	for i := range p.Arrangements {
		if p.Arrangements[i].ID == arrangementID {
			arr = &p.Arrangements[i]
			break
		}
	}
	lanes := collectLanes(p, arrangementID)

	byFamily := map[GroupKey][]*lane{}
	for _, k := range lanes.keys {
		l := lanes.m[k]
		byFamily[l.group.Key] = append(byFamily[l.group.Key], l)
	}
	ordered := map[GroupKey][]*lane{}
	for family, list := range byFamily {
		nonAuto := []*lane{}
		for _, l := range list {
			if !l.isAutomation {
				nonAuto = append(nonAuto, l)
			}
		}
		sort.SliceStable(nonAuto, func(a, b int) bool { return nonAuto[a].sortKey < nonAuto[b].sortKey })
		autosByTarget := map[int][]*lane{}
		targetOrder := []int{}
		orphans := []*lane{}
		for _, l := range list {
			if !l.isAutomation {
				continue
			}
			if l.automationTargetIid != nil {
				t := *l.automationTargetIid
				if _, ok := autosByTarget[t]; !ok {
					targetOrder = append(targetOrder, t)
				}
				autosByTarget[t] = append(autosByTarget[t], l)
			} else {
				orphans = append(orphans, l)
			}
		}
		for _, arrL := range autosByTarget {
			sort.SliceStable(arrL, func(a, b int) bool { return arrL[a].sortKey < arrL[b].sortKey })
		}
		sort.SliceStable(orphans, func(a, b int) bool { return orphans[a].sortKey < orphans[b].sortKey })
		out := []*lane{}
		consumed := map[int]bool{}
		for _, parent := range nonAuto {
			out = append(out, parent)
			if parent.kind != "channel" {
				continue
			}
			if children, ok := autosByTarget[parent.refID]; ok && !consumed[parent.refID] {
				out = append(out, children...)
				consumed[parent.refID] = true
			}
		}
		for _, t := range targetOrder {
			if !consumed[t] {
				out = append(out, autosByTarget[t]...)
			}
		}
		out = append(out, orphans...)
		ordered[family] = out
	}

	tracks := []TrackMutation{}
	laneToTrack := map[string]int{}
	cursor := 0
	for _, family := range FamilyOrder {
		fl := ordered[family]
		if len(fl) == 0 {
			continue
		}
		if addSeparators {
			fam := FamilyLabels[family]
			tracks = append(tracks, TrackMutation{TrackIndex: cursor, Name: "[" + fam.Label + "]", RGB: fam.RGB, Grouped: false, IsFamilySeparator: true})
			cursor++
		}
		for _, l := range fl {
			grouped := l.isAutomation && l.automationTargetIid != nil
			tracks = append(tracks, TrackMutation{TrackIndex: cursor, Name: l.displayName, RGB: l.group.RGB, Grouped: grouped})
			laneToTrack[fmt.Sprintf("%s:%d", l.kind, l.refID)] = cursor
			cursor++
		}
	}

	moves := []ClipMoveMutation{}
	if arr != nil {
		seen := map[string]bool{}
		for _, k := range lanes.keys {
			l := lanes.m[k]
			target, ok := laneToTrack[k]
			if !ok {
				continue
			}
			for _, idx := range l.clipIndices {
				clip := arr.Clips[idx]
				from := clipTrackMax - clip.TrackRvidx
				if from == target {
					continue
				}
				mk := fmt.Sprintf("%d|%s|%d", from, l.kind, l.refID)
				if seen[mk] {
					continue
				}
				seen[mk] = true
				moves = append(moves, ClipMoveMutation{FromTrackIndex: from, FromPositionTicks: clip.Position, RefID: l.refID, RefKind: l.kind, ToTrackIndex: target})
			}
		}
	}
	return ReorganizePlan{ArrangementID: arrangementID, Tracks: tracks, ClipMoves: moves}
}

// ApplyReorganize applies a plan to a project.
func ApplyReorganize(p *FLPProject, plan ReorganizePlan) (res *FLPProject, err error) {
	defer catchMut(&err)
	cur := p
	for _, mv := range plan.ClipMoves {
		ref := float64(mv.RefID)
		to := float64(mv.ToTrackIndex)
		cur = must(MoveClip(cur, plan.ArrangementID,
			ClipMatch{TrackIndex: float64(mv.FromTrackIndex), RefID: &ref, Kind: mv.RefKind},
			ClipDest{TrackIndex: &to}))
	}
	for _, t := range plan.Tracks {
		cur = must(SetTrackName(cur, plan.ArrangementID, t.TrackIndex, t.Name))
		cur = must(SetTrackColor(cur, plan.ArrangementID, t.TrackIndex, MutRGBA{R: float64(t.RGB.R), G: float64(t.RGB.G), B: float64(t.RGB.B), A: 0}))
		cur = must(SetTrackGrouped(cur, plan.ArrangementID, t.TrackIndex, t.Grouped))
	}
	return cur, nil
}

// ReorganizeProject plans and applies; returns the new project, plan and mutation count.
func ReorganizeProject(p *FLPProject, opts ReorganizeOptions) (*FLPProject, ReorganizePlan, int, error) {
	plan := PlanReorganize(p, opts)
	applied := len(plan.Tracks)*3 + len(plan.ClipMoves)
	out, err := ApplyReorganize(p, plan)
	return out, plan, applied, err
}
