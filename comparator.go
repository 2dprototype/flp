package flp

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
)

// ───────────── formatting primitives ─────────────

// isNilVal reports whether v is a nil interface or a nil pointer.
func isNilVal(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}

// Classify returns the scalar change kind from an old/new pair (nil == absent).
func Classify(oldVal, newVal interface{}) ChangeKind {
	on, nn := isNilVal(oldVal), isNilVal(newVal)
	if on && !nn {
		return KindAdded
	}
	if !on && nn {
		return KindRemoved
	}
	return KindModified
}

// FmtNoneFriendly renders nil as "unset"; strings are quoted like Python repr.
func FmtNoneFriendly(value interface{}) string {
	if isNilVal(value) {
		return "unset"
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		return "'" + rv.String() + "'"
	case reflect.Bool:
		if rv.Bool() {
			return "True"
		}
		return "False"
	case reflect.Float32, reflect.Float64:
		return jsNum(rv.Float())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", rv.Uint())
	}
	return fmt.Sprint(rv.Interface())
}

// PythonFloatRepr mimics Python's repr(float): whole numbers show ".0".
func PythonFloatRepr(n float64) string {
	s := jsNum(n)
	if strings.ContainsAny(s, ".eE") {
		return s
	}
	return s + ".0"
}

func FmtPct(v *float64) string {
	if v == nil {
		return "unset"
	}
	return jsNum(pythonRound(*v*100)) + "%"
}

func FmtPan(v *float64) string {
	if v == nil {
		return "unset"
	}
	if math.Abs(*v) < 1e-6 {
		return "centered"
	}
	side := "R"
	if *v < 0 {
		side = "L"
	}
	return jsNum(pythonRound(math.Abs(*v)*100)) + "% " + side
}

func FmtTimeSig(ts *TimeSignatureJson) string {
	if ts == nil {
		return "unset"
	}
	return fmt.Sprintf("%d/%d", ts.Numerator, ts.Denominator)
}

func FmtBool(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// ColorHex renders an RGBA JSON value as #rrggbb.
func ColorHex(c *RgbaJson) string {
	if c == nil {
		return "unset"
	}
	return fmt.Sprintf("#%02x%02x%02x", int(pythonRound(c.Red*255)), int(pythonRound(c.Green*255)), int(pythonRound(c.Blue*255)))
}

// deepEqual compares two values treating nil interfaces and nil pointers as equal.
func deepEqual(a, b interface{}) bool {
	an, bn := isNilVal(a), isNilVal(b)
	if an || bn {
		return an && bn
	}
	return reflect.DeepEqual(a, b)
}

func ptrEq[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ScalarChange emits a Change if oldVal != newVal, else nil.
func ScalarChange(path string, oldVal, newVal interface{}, humanLabel string) *Change {
	if deepEqual(oldVal, newVal) {
		return nil
	}
	c := MakeChange(Change{Path: path, Kind: Classify(oldVal, newVal), OldValue: oldVal, NewValue: newVal, HumanLabel: humanLabel})
	return &c
}

func pushChange(changes *[]Change, c *Change) {
	if c != nil {
		*changes = append(*changes, *c)
	}
}

// ───────────── metadata ─────────────

// CompareMetadata diffs two metadata records field by field.
func CompareMetadata(oldM, newM MetadataJson) []Change {
	changes := []Change{}
	base := "metadata"

	if oldM.Tempo != newM.Tempo {
		delta := newM.Tempo - oldM.Tempo
		direction := "decreased"
		if delta > 0 {
			direction = "increased"
		}
		label := fmt.Sprintf("Tempo %s from %s to %s BPM", direction, PythonFloatRepr(oldM.Tempo), PythonFloatRepr(newM.Tempo))
		changes = append(changes, MakeChange(Change{Path: base + ".tempo", Kind: KindModified, OldValue: oldM.Tempo, NewValue: newM.Tempo, HumanLabel: label}))
	}

	if !deepEqual(oldM.TimeSignature, newM.TimeSignature) {
		label := fmt.Sprintf("Time signature changed from %s to %s", FmtTimeSig(oldM.TimeSignature), FmtTimeSig(newM.TimeSignature))
		changes = append(changes, MakeChange(Change{
			Path: base + ".time_signature", Kind: Classify(oldM.TimeSignature, newM.TimeSignature),
			OldValue: oldM.TimeSignature, NewValue: newM.TimeSignature, HumanLabel: label,
		}))
	}

	if oldM.PPQ != newM.PPQ {
		pushChange(&changes, ScalarChange(base+".ppq", oldM.PPQ, newM.PPQ, fmt.Sprintf("PPQ changed from %d to %d", oldM.PPQ, newM.PPQ)))
	}

	type strField struct {
		field, label string
		ov, nv       interface{}
	}
	fields := []strField{
		{"title", "Title", oldM.Title, newM.Title},
		{"artists", "Artists", oldM.Artists, newM.Artists},
		{"genre", "Genre", oldM.Genre, newM.Genre},
		{"comments", "Comments", oldM.Comments, newM.Comments},
		{"url", "URL", oldM.URL, newM.URL},
	}
	for _, f := range fields {
		if deepEqual(f.ov, f.nv) {
			continue
		}
		changes = append(changes, MakeChange(Change{
			Path: base + "." + f.field, Kind: Classify(f.ov, f.nv), OldValue: f.ov, NewValue: f.nv,
			HumanLabel: fmt.Sprintf("%s: %s → %s", f.label, FmtNoneFriendly(f.ov), FmtNoneFriendly(f.nv)),
		}))
	}

	if !deepEqual(oldM.DataPath, newM.DataPath) {
		var ov, nv *string
		if oldM.DataPath != nil {
			ov = &oldM.DataPath.Value
		}
		if newM.DataPath != nil {
			nv = &newM.DataPath.Value
		}
		changes = append(changes, MakeChange(Change{
			Path: base + ".data_path", Kind: Classify(oldM.DataPath, newM.DataPath),
			OldValue: oldM.DataPath, NewValue: newM.DataPath,
			HumanLabel: fmt.Sprintf("Data path: %s → %s", FmtNoneFriendly(ov), FmtNoneFriendly(nv)),
		}))
	}

	if oldM.Looped != newM.Looped {
		pushChange(&changes, ScalarChange(base+".looped", oldM.Looped, newM.Looped,
			fmt.Sprintf("Loop playback %s (was %s)", FmtBool(newM.Looped), FmtBool(oldM.Looped))))
	}
	if oldM.MainPitch != newM.MainPitch {
		pushChange(&changes, ScalarChange(base+".main_pitch", oldM.MainPitch, newM.MainPitch,
			fmt.Sprintf("Main pitch changed from %d to %d", oldM.MainPitch, newM.MainPitch)))
	}
	if !ptrEq(oldM.MainVolume, newM.MainVolume) {
		pushChange(&changes, ScalarChange(base+".main_volume", oldM.MainVolume, newM.MainVolume,
			fmt.Sprintf("Main volume: %s → %s", FmtNoneFriendly(oldM.MainVolume), FmtNoneFriendly(newM.MainVolume))))
	}
	if oldM.PanLaw != newM.PanLaw {
		pushChange(&changes, ScalarChange(base+".pan_law", oldM.PanLaw, newM.PanLaw,
			fmt.Sprintf("Pan law changed from %d to %d", oldM.PanLaw, newM.PanLaw)))
	}
	if oldM.ShowInfo != newM.ShowInfo {
		pushChange(&changes, ScalarChange(base+".show_info", oldM.ShowInfo, newM.ShowInfo,
			fmt.Sprintf("Show-info %s (was %s)", FmtBool(newM.ShowInfo), FmtBool(oldM.ShowInfo))))
	}
	return changes
}

// ───────────── channels / plugins ─────────────

func channelLabel(ch ChannelJson) string {
	name := fmt.Sprintf("#%d", ch.Iid)
	if ch.Name != nil {
		name = *ch.Name
	}
	return fmt.Sprintf("%s '%s'", ch.Kind, name)
}

func channelLabelWithSample(ch ChannelJson) string {
	base := channelLabel(ch)
	if ch.SamplePath == nil {
		return base
	}
	return fmt.Sprintf("%s (sample: %s)", base, ch.SamplePath.Value)
}

func pluginDisplayLabel(p *PluginJson) string {
	name := p.Name
	if name == "" {
		name = "unknown plugin"
	}
	if p.IsVst {
		if p.Vendor != nil && *p.Vendor != "" {
			return fmt.Sprintf("'%s' (%s, VST)", name, *p.Vendor)
		}
		return fmt.Sprintf("'%s' (VST)", name)
	}
	return name
}

// ComparePluginLabels compares two plugin JSON objects at pathPrefix.
func ComparePluginLabels(pathPrefix string, oldP, newP *PluginJson, slotHint string) []Change {
	return comparePlugin(pathPrefix, oldP, newP, slotHint)
}

func comparePlugin(pathPrefix string, oldP, newP *PluginJson, slotHint string) []Change {
	out := []Change{}
	if oldP == nil && newP == nil {
		return out
	}
	if oldP == nil {
		out = append(out, MakeChange(Change{
			Path: pathPrefix, Kind: KindAdded, OldValue: nil, NewValue: newP,
			HumanLabel: fmt.Sprintf("Plugin added%s: %s", slotHint, pluginDisplayLabel(newP)),
		}))
		return out
	}
	if newP == nil {
		out = append(out, MakeChange(Change{
			Path: pathPrefix, Kind: KindRemoved, OldValue: oldP, NewValue: nil,
			HumanLabel: fmt.Sprintf("Plugin removed%s: %s", slotHint, pluginDisplayLabel(oldP)),
		}))
		return out
	}
	if oldP.Name != newP.Name {
		out = append(out, MakeChange(Change{
			Path: pathPrefix + ".name", Kind: KindModified, OldValue: oldP.Name, NewValue: newP.Name,
			HumanLabel: fmt.Sprintf("Plugin swapped%s: %s → %s", slotHint, pluginDisplayLabel(oldP), pluginDisplayLabel(newP)),
		}))
	} else {
		if !ptrEq(oldP.Vendor, newP.Vendor) {
			out = append(out, MakeChange(Change{
				Path: pathPrefix + ".vendor", Kind: Classify(oldP.Vendor, newP.Vendor), OldValue: oldP.Vendor, NewValue: newP.Vendor,
				HumanLabel: fmt.Sprintf("Plugin vendor%s: %s → %s", slotHint, FmtNoneFriendly(oldP.Vendor), FmtNoneFriendly(newP.Vendor)),
			}))
		}
		if oldP.IsVst != newP.IsVst {
			host := "native"
			if newP.IsVst {
				host = "VST"
			}
			out = append(out, MakeChange(Change{
				Path: pathPrefix + ".is_vst", Kind: KindModified, OldValue: oldP.IsVst, NewValue: newP.IsVst,
				HumanLabel: fmt.Sprintf("Plugin hosting%s changed: %s", slotHint, host),
			}))
		}
	}
	return out
}

// CompareChannel produces a ChannelDiff for one matched (or unmatched) pair.
func CompareChannel(match Match[ChannelJson], ppq int) ChannelDiff {
	if match.Old == nil && match.New != nil {
		return MakeChannelDiff(ChannelDiff{
			Identity: []interface{}{"channel", match.New.Iid}, Kind: KindAdded, Name: match.New.Name,
			HumanLabel: "Added channel " + channelLabelWithSample(*match.New),
		})
	}
	if match.Old != nil && match.New == nil {
		return MakeChannelDiff(ChannelDiff{
			Identity: []interface{}{"channel", match.Old.Iid}, Kind: KindRemoved, Name: match.Old.Name,
			HumanLabel: "Removed channel " + channelLabelWithSample(*match.Old),
		})
	}
	oldCh, newCh := *match.Old, *match.New
	path := fmt.Sprintf("channels[%d]", oldCh.Iid)
	changes := []Change{}

	if oldCh.Kind != newCh.Kind {
		pushChange(&changes, ScalarChange(path+".kind", oldCh.Kind, newCh.Kind, fmt.Sprintf("Channel type changed from %s to %s", oldCh.Kind, newCh.Kind)))
	}
	if !ptrEq(oldCh.Name, newCh.Name) {
		pushChange(&changes, ScalarChange(path+".name", oldCh.Name, newCh.Name,
			fmt.Sprintf("Channel renamed from %s to %s", FmtNoneFriendly(oldCh.Name), FmtNoneFriendly(newCh.Name))))
	}
	if !deepEqual(oldCh.Color, newCh.Color) {
		pushChange(&changes, ScalarChange(path+".color", oldCh.Color, newCh.Color,
			fmt.Sprintf("Channel color: %s → %s", ColorHex(oldCh.Color), ColorHex(newCh.Color))))
	}
	if oldCh.Enabled != newCh.Enabled {
		pushChange(&changes, ScalarChange(path+".enabled", oldCh.Enabled, newCh.Enabled,
			fmt.Sprintf("Channel %s (was %s)", FmtBool(newCh.Enabled), FmtBool(oldCh.Enabled))))
	}
	if oldCh.Muted != newCh.Muted {
		w := "unmuted"
		if newCh.Muted {
			w = "muted"
		}
		pushChange(&changes, ScalarChange(path+".muted", oldCh.Muted, newCh.Muted, "Channel "+w))
	}
	if oldCh.Volume != newCh.Volume {
		ov, nv := oldCh.Volume, newCh.Volume
		pushChange(&changes, ScalarChange(path+".volume", oldCh.Volume, newCh.Volume,
			fmt.Sprintf("Channel volume %s → %s", FmtPct(&ov), FmtPct(&nv))))
	}
	if oldCh.Pan != newCh.Pan {
		ov, nv := oldCh.Pan, newCh.Pan
		pushChange(&changes, ScalarChange(path+".pan", oldCh.Pan, newCh.Pan,
			fmt.Sprintf("Channel pan %s → %s", FmtPan(&ov), FmtPan(&nv))))
	}
	if !ptrEq(oldCh.TargetInsert, newCh.TargetInsert) {
		pushChange(&changes, ScalarChange(path+".target_insert", oldCh.TargetInsert, newCh.TargetInsert,
			fmt.Sprintf("Channel routed to insert %s (was %s)", FmtNoneFriendly(newCh.TargetInsert), FmtNoneFriendly(oldCh.TargetInsert))))
	}
	if !deepEqual(oldCh.SamplePath, newCh.SamplePath) {
		var ov, nv *string
		if oldCh.SamplePath != nil {
			ov = &oldCh.SamplePath.Value
		}
		if newCh.SamplePath != nil {
			nv = &newCh.SamplePath.Value
		}
		pushChange(&changes, ScalarChange(path+".sample_path", oldCh.SamplePath, newCh.SamplePath,
			fmt.Sprintf("Sample path: %s → %s", FmtNoneFriendly(ov), FmtNoneFriendly(nv))))
	}

	changes = append(changes, comparePlugin(path+".plugin", oldCh.Plugin, newCh.Plugin, "")...)

	automationChanges := []AutomationChange{}
	if oldCh.Kind == "automation" && newCh.Kind == "automation" {
		automationChanges = DiffAutomationPoints(oldCh.AutomationPoints, newCh.AutomationPoints, ppq)
	}

	nTotal := len(changes) + len(automationChanges)
	var label string
	if nTotal > 0 {
		label = fmt.Sprintf("Channel %s modified (%d changes)", channelLabel(oldCh), nTotal)
	} else {
		label = fmt.Sprintf("Channel %s unchanged", channelLabel(oldCh))
	}
	return MakeChannelDiff(ChannelDiff{
		Identity: []interface{}{"channel", oldCh.Iid}, Kind: KindModified, Name: oldCh.Name,
		HumanLabel: label, Changes: changes, AutomationChanges: automationChanges,
	})
}

// ───────────── patterns ─────────────

func patternLabel(p PatternJson) string {
	name := fmt.Sprintf("#%d", p.Iid)
	if p.Name != nil {
		name = *p.Name
	}
	return fmt.Sprintf("pattern '%s'", name)
}

// capitalize mimics Python's str.capitalize(): first char upper, the rest lower.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	out := make([]rune, len(r))
	out[0] = unicode.ToUpper(r[0])
	for i := 1; i < len(r); i++ {
		out[i] = unicode.ToLower(r[i])
	}
	return string(out)
}

// ComparePattern produces a PatternDiff for one matched (or unmatched) pair.
func ComparePattern(match Match[PatternJson], ppq int) PatternDiff {
	if match.Old == nil && match.New != nil {
		return MakePatternDiff(PatternDiff{
			Identity: []interface{}{"pattern", match.New.Iid}, Kind: KindAdded, Name: match.New.Name,
			HumanLabel: fmt.Sprintf("Added %s (%d notes)", patternLabel(*match.New), len(match.New.Notes)),
		})
	}
	if match.Old != nil && match.New == nil {
		return MakePatternDiff(PatternDiff{
			Identity: []interface{}{"pattern", match.Old.Iid}, Kind: KindRemoved, Name: match.Old.Name,
			HumanLabel: fmt.Sprintf("Removed %s (%d notes)", patternLabel(*match.Old), len(match.Old.Notes)),
		})
	}
	oldP, newP := *match.Old, *match.New
	path := fmt.Sprintf("patterns[%d]", oldP.Iid)
	changes := []Change{}

	if !ptrEq(oldP.Name, newP.Name) {
		pushChange(&changes, ScalarChange(path+".name", oldP.Name, newP.Name,
			fmt.Sprintf("Pattern renamed from %s to %s", FmtNoneFriendly(oldP.Name), FmtNoneFriendly(newP.Name))))
	}
	if !deepEqual(oldP.Color, newP.Color) {
		pushChange(&changes, ScalarChange(path+".color", oldP.Color, newP.Color,
			fmt.Sprintf("Pattern color: %s → %s", ColorHex(oldP.Color), ColorHex(newP.Color))))
	}
	if !ptrEq(oldP.Length, newP.Length) {
		pushChange(&changes, ScalarChange(path+".length", oldP.Length, newP.Length,
			fmt.Sprintf("Pattern length: %s → %s ticks", FmtNoneFriendly(oldP.Length), FmtNoneFriendly(newP.Length))))
	}
	if oldP.Looped != newP.Looped {
		pushChange(&changes, ScalarChange(path+".looped", oldP.Looped, newP.Looped,
			fmt.Sprintf("Pattern loop %s (was %s)", FmtBool(newP.Looped), FmtBool(oldP.Looped))))
	}

	noteChanges := DiffNotes(oldP.Notes, newP.Notes, ppq)
	controllerChanges := DiffAutomationPoints(oldP.Controllers, newP.Controllers, ppq)

	nChanges := len(changes) + len(noteChanges) + len(controllerChanges)
	var label string
	if nChanges > 0 {
		label = fmt.Sprintf("%s modified (%d changes)", capitalize(patternLabel(oldP)), nChanges)
	} else {
		label = fmt.Sprintf("%s unchanged", capitalize(patternLabel(oldP)))
	}
	return MakePatternDiff(PatternDiff{
		Identity: []interface{}{"pattern", oldP.Iid}, Kind: KindModified, Name: oldP.Name,
		HumanLabel: label, Changes: changes, NoteChanges: noteChanges, ControllerChanges: controllerChanges,
	})
}

// ───────────── orchestrator ─────────────

func renderCountsSummary(c SummaryCounts, noteCount, automationCount, trackCount int) string {
	if c.Total == 0 && noteCount == 0 && automationCount == 0 && trackCount == 0 {
		return "No changes"
	}
	parts := []string{}
	add := func(n int, key string) {
		if n != 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, key))
		}
	}
	add(c.Metadata, "metadata")
	add(c.Channels, "channels")
	add(c.Patterns, "patterns")
	add(c.Mixer, "mixer")
	add(c.Arrangements, "arrangements")
	add(c.Opaque, "opaque")
	if trackCount != 0 {
		parts = append(parts, fmt.Sprintf("%d tracks", trackCount))
	}
	if noteCount != 0 {
		parts = append(parts, fmt.Sprintf("%d notes", noteCount))
	}
	if automationCount != 0 {
		parts = append(parts, fmt.Sprintf("%d automation keyframes", automationCount))
	}
	return fmt.Sprintf("%d changes (%s)", c.Total, strings.Join(parts, ", "))
}

// CompareProjectsJson compares two projects at the FlpInfoJson level.
func CompareProjectsJson(oldJ, newJ FlpInfoJson) DiffResult {
	ppq := newJ.Metadata.PPQ
	if ppq == 0 {
		ppq = oldJ.Metadata.PPQ
	}
	if ppq == 0 {
		ppq = 96
	}

	metadataChanges := CompareMetadata(oldJ.Metadata, newJ.Metadata)

	channelMatches := pairByKey(oldJ.Channels, newJ.Channels,
		func(c *ChannelJson) string { return fmt.Sprint(c.Iid) },
		func(c *ChannelJson) (string, bool) {
			if c.Name != nil && *c.Name != "" {
				return c.Kind + "\x00" + *c.Name, true
			}
			return "", false
		})
	channelChanges := []ChannelDiff{}
	for _, m := range channelMatches {
		d := CompareChannel(m, ppq)
		if d.Kind == KindAdded || d.Kind == KindRemoved || len(d.Changes) > 0 || len(d.AutomationChanges) > 0 {
			channelChanges = append(channelChanges, d)
		}
	}

	patternMatches := pairByKey(oldJ.Patterns, newJ.Patterns,
		func(p *PatternJson) string { return fmt.Sprint(p.Iid) },
		func(p *PatternJson) (string, bool) { return optName(p.Name) })
	patternChanges := []PatternDiff{}
	for _, m := range patternMatches {
		d := ComparePattern(m, ppq)
		if d.Kind == KindAdded || d.Kind == KindRemoved || len(d.Changes) > 0 || len(d.NoteChanges) > 0 || len(d.ControllerChanges) > 0 {
			patternChanges = append(patternChanges, d)
		}
	}

	mixerChanges := CompareMixerFromJson(oldJ.Mixer.Inserts, newJ.Mixer.Inserts)

	channelsByIid := map[int]*ChannelJson{}
	for i := range oldJ.Channels {
		channelsByIid[oldJ.Channels[i].Iid] = &oldJ.Channels[i]
	}
	for i := range newJ.Channels {
		channelsByIid[newJ.Channels[i].Iid] = &newJ.Channels[i]
	}
	patternsByIid := map[int]*PatternJson{}
	for i := range oldJ.Patterns {
		patternsByIid[oldJ.Patterns[i].Iid] = &oldJ.Patterns[i]
	}
	for i := range newJ.Patterns {
		patternsByIid[newJ.Patterns[i].Iid] = &newJ.Patterns[i]
	}

	arrangementMatches := pairByKey(oldJ.Arrangements, newJ.Arrangements,
		func(a *ArrangementJson) string { return fmt.Sprint(a.Index) },
		func(a *ArrangementJson) (string, bool) { return optName(a.Name) })
	arrangementChanges := []ArrangementDiff{}
	for _, m := range arrangementMatches {
		d := CompareArrangement(m, channelsByIid, patternsByIid, ppq)
		if d.Kind == KindAdded || d.Kind == KindRemoved || len(d.Changes) > 0 || len(d.TrackChanges) > 0 {
			arrangementChanges = append(arrangementChanges, d)
		}
	}

	opaqueChanges := []OpaqueChange{}
	counts := ComputeSummaryCounts(metadataChanges, channelChanges, patternChanges, mixerChanges, arrangementChanges, opaqueChanges)
	noteCount, automationCount, trackCount := 0, 0, 0
	for _, pd := range patternChanges {
		noteCount += len(pd.NoteChanges)
		automationCount += len(pd.ControllerChanges)
	}
	for _, ad := range arrangementChanges {
		trackCount += len(ad.TrackChanges)
	}
	summary := MakeDiffSummary(DiffSummary{
		TotalChanges: counts.Total, MetadataChanges: counts.Metadata, ChannelChanges: counts.Channels,
		PatternChanges: counts.Patterns, MixerChanges: counts.Mixer, ArrangementChanges: counts.Arrangements,
		OpaqueChanges: counts.Opaque, NoteChanges: noteCount, AutomationChanges: automationCount,
		TrackChanges: trackCount, HumanLabel: renderCountsSummary(counts, noteCount, automationCount, trackCount),
	})
	return DiffResult{
		Summary: summary, MetadataChanges: metadataChanges, ChannelChanges: channelChanges,
		PatternChanges: patternChanges, MixerChanges: mixerChanges, ArrangementChanges: arrangementChanges,
		OpaqueChanges: opaqueChanges,
	}
}

// CompareProjects projects both sides via ToFlpInfoJson and diffs them.
func CompareProjects(oldProj, newProj *FLPProject) DiffResult {
	return CompareProjectsJson(ToFlpInfoJson(oldProj), ToFlpInfoJson(newProj))
}
