package flpdiff

import (
	"fmt"
	"strings"
)

// CanonicalHeader is the first line of canonical output.
const CanonicalHeader = "# flp canonical v1"

// fmtFloat: N decimal places then strip trailing zeros and the decimal point.
func fmtFloat(v float64, places int) string {
	text := jsToFixed(v, places)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(text, "0")
		text = strings.TrimSuffix(text, ".")
		if text == "" {
			text = "0"
		}
	}
	return text
}

func fmtFloatPtr(v *float64, places int) *string {
	if v == nil {
		return nil
	}
	s := fmtFloat(*v, places)
	return &s
}

func fmtColorCanon(c *RgbaJson) *string {
	if c == nil {
		return nil
	}
	s := fmt.Sprintf("#%02x%02x%02x", int(jsRound(c.Red*255)), int(jsRound(c.Green*255)), int(jsRound(c.Blue*255)))
	return &s
}

func fmtPathCanon(p *PathJson) *string {
	if p == nil {
		return nil
	}
	return &p.Value
}

func emitKv(lines *[]string, key string, value *string, indent int) {
	if value == nil || *value == "" {
		return
	}
	*lines = append(*lines, strings.Repeat(" ", indent)+key+": "+*value)
}

func emitKvS(lines *[]string, key, value string, indent int) {
	emitKv(lines, key, &value, indent)
}

func intPtrStr(v *int) *string {
	if v == nil {
		return nil
	}
	s := fmt.Sprint(*v)
	return &s
}

func fmtVersionJSON(v FLVersionJson) *string {
	parts := []string{fmt.Sprint(v.Major), fmt.Sprint(v.Minor), fmt.Sprint(v.Patch)}
	if v.Build != nil {
		parts = append(parts, fmt.Sprint(*v.Build))
	}
	s := strings.Join(parts, ".")
	return &s
}

// RenderCanonical returns the deterministic line-oriented canonical text of a project.
func RenderCanonical(p *FLPProject) string {
	json := ToFlpInfoJson(p)
	lines := []string{CanonicalHeader}
	emitCanonMetadata(&lines, json.Metadata)
	if len(json.Channels) > 0 {
		lines = append(lines, "", "## channels")
		for _, ch := range json.Channels {
			emitCanonChannel(&lines, ch)
		}
	}
	if len(json.Patterns) > 0 {
		lines = append(lines, "", "## patterns")
		for _, pt := range json.Patterns {
			emitCanonPattern(&lines, pt)
		}
	}
	if len(json.Mixer.Inserts) > 0 {
		lines = append(lines, "", "## mixer")
		for _, ins := range json.Mixer.Inserts {
			emitCanonInsert(&lines, ins)
		}
	}
	if len(json.Arrangements) > 0 {
		lines = append(lines, "", "## arrangements")
		for _, a := range json.Arrangements {
			emitCanonArrangement(&lines, a)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func emitCanonMetadata(lines *[]string, md MetadataJson) {
	*lines = append(*lines, "", "## metadata")
	emitKvS(lines, "format", md.Format, 0)
	emitKv(lines, "version", fmtVersionJSON(md.Version), 0)
	emitKvS(lines, "ppq", fmt.Sprint(md.PPQ), 0)
	emitKv(lines, "tempo", fmtFloatPtr(&md.Tempo, 3), 0)
	if md.TimeSignature != nil {
		emitKvS(lines, "time_signature", fmt.Sprintf("%d/%d", md.TimeSignature.Numerator, md.TimeSignature.Denominator), 0)
	}
	emitKvS(lines, "title", md.Title, 0)
	emitKvS(lines, "artists", md.Artists, 0)
	emitKvS(lines, "genre", md.Genre, 0)
	emitKvS(lines, "comments", md.Comments, 0)
	emitKv(lines, "url", md.URL, 0)
	emitKv(lines, "data_path", fmtPathCanon(md.DataPath), 0)
	if md.Looped {
		*lines = append(*lines, "looped: True")
	}
	if md.MainPitch != 0 {
		emitKvS(lines, "main_pitch", fmt.Sprint(md.MainPitch), 0)
	}
	emitKv(lines, "main_volume", intPtrStr(md.MainVolume), 0)
	if md.PanLaw != 0 {
		emitKvS(lines, "pan_law", fmt.Sprint(md.PanLaw), 0)
	}
}

func emitCanonChannel(lines *[]string, ch ChannelJson) {
	*lines = append(*lines, "", fmt.Sprintf("### channel %d [%s]", ch.Iid, ch.Kind))
	emitKv(lines, "name", ch.Name, 0)
	emitKv(lines, "color", fmtColorCanon(ch.Color), 0)
	if !ch.Enabled {
		*lines = append(*lines, "disabled: true")
	}
	if ch.Muted {
		*lines = append(*lines, "muted: true")
	}
	emitKv(lines, "volume", fmtFloatPtr(&ch.Volume, 3), 0)
	emitKv(lines, "pan", fmtFloatPtr(&ch.Pan, 3), 0)
	emitKv(lines, "target_insert", intPtrStr(ch.TargetInsert), 0)
	emitKv(lines, "sample_path", fmtPathCanon(ch.SamplePath), 0)
	if ch.Plugin != nil {
		emitCanonPlugin(lines, ch.Plugin, "plugin")
	}
}

func emitCanonPlugin(lines *[]string, p *PluginJson, prefix string) {
	*lines = append(*lines, prefix+":")
	emitKvS(lines, "  name", p.Name, 0)
	emitKv(lines, "  vendor", p.Vendor, 0)
	if p.IsVst {
		*lines = append(*lines, "  is_vst: true")
	}
}

func emitCanonPattern(lines *[]string, p PatternJson) {
	*lines = append(*lines, "", fmt.Sprintf("### pattern %d", p.Iid))
	emitKv(lines, "name", p.Name, 0)
	emitKv(lines, "color", fmtColorCanon(p.Color), 0)
	if p.Length != nil {
		emitKvS(lines, "length", fmt.Sprint(*p.Length), 0)
	}
	if p.Looped {
		*lines = append(*lines, "looped: true")
	}
	if len(p.Notes) > 0 {
		*lines = append(*lines, fmt.Sprintf("notes: %d", len(p.Notes)))
		for _, n := range p.Notes {
			parts := []string{
				fmt.Sprintf("ch=%d", n.ChannelIid), fmt.Sprintf("pos=%d", n.Position), fmt.Sprintf("key=%d", n.Key),
				fmt.Sprintf("len=%d", n.Length), fmt.Sprintf("vel=%d", n.Velocity),
			}
			if n.Pan != 0 {
				parts = append(parts, fmt.Sprintf("pan=%d", n.Pan))
			}
			if n.Release != 0 {
				parts = append(parts, fmt.Sprintf("rel=%d", n.Release))
			}
			if n.FinePitch != 0 {
				parts = append(parts, fmt.Sprintf("fp=%d", n.FinePitch))
			}
			*lines = append(*lines, "  - note "+strings.Join(parts, " "))
		}
	}
	if len(p.Controllers) > 0 {
		*lines = append(*lines, fmt.Sprintf("controllers: %d", len(p.Controllers)))
		for _, ap := range p.Controllers {
			parts := []string{"pos=" + jsNum(ap.Position), "val=" + fmtFloat(ap.Value, 4)}
			if ap.Tension != 0 {
				parts = append(parts, "tension="+fmtFloat(ap.Tension, 4))
			}
			*lines = append(*lines, "  - keyframe "+strings.Join(parts, " "))
		}
	}
}

func isInterestingInsert(ins MixerInsertJson) bool {
	hasPlugin := false
	for _, s := range ins.Slots {
		if s.Plugin != nil {
			hasPlugin = true
			break
		}
	}
	boring := (ins.Name == nil || *ins.Name == "") && ins.Color == nil && ins.Volume == nil && ins.Pan == nil &&
		ins.StereoSeparation == nil && ins.Enabled && !ins.Locked && len(ins.RoutesTo) == 0 && !hasPlugin
	return !boring
}

func emitCanonInsert(lines *[]string, ins MixerInsertJson) {
	if !isInterestingInsert(ins) {
		return
	}
	*lines = append(*lines, "", fmt.Sprintf("### insert %d", ins.Index))
	emitKv(lines, "name", ins.Name, 0)
	emitKv(lines, "color", fmtColorCanon(ins.Color), 0)
	emitKv(lines, "volume", fmtFloatPtr(ins.Volume, 3), 0)
	emitKv(lines, "pan", fmtFloatPtr(ins.Pan, 3), 0)
	emitKv(lines, "stereo_separation", fmtFloatPtr(ins.StereoSeparation, 3), 0)
	if !ins.Enabled {
		*lines = append(*lines, "enabled: false")
	}
	if ins.Locked {
		*lines = append(*lines, "locked: true")
	}
	if len(ins.RoutesTo) > 0 {
		*lines = append(*lines, "routes_to: "+pythonIntListRepr(ins.RoutesTo))
	}
	for _, s := range ins.Slots {
		if s.Plugin == nil && s.Enabled {
			continue
		}
		*lines = append(*lines, fmt.Sprintf("slot %d:", s.Index))
		if !s.Enabled {
			*lines = append(*lines, "  enabled: false")
		}
		if s.Plugin != nil {
			emitCanonPlugin(lines, s.Plugin, "  plugin")
		}
	}
}

func emitCanonArrangement(lines *[]string, a ArrangementJson) {
	*lines = append(*lines, "", fmt.Sprintf("### arrangement %d", a.Index))
	emitKv(lines, "name", a.Name, 0)
	for _, tr := range a.Tracks {
		emitCanonTrack(lines, tr)
	}
	for _, tm := range a.TimeMarkers {
		parts := []string{fmt.Sprintf("pos=%d", tm.Position)}
		if tm.Name != nil && *tm.Name != "" {
			parts = append(parts, "name="+jsonString(*tm.Name))
		}
		if tm.Numerator != nil && tm.Denominator != nil {
			parts = append(parts, fmt.Sprintf("time_sig=%d/%d", *tm.Numerator, *tm.Denominator))
		}
		*lines = append(*lines, "timemarker "+strings.Join(parts, " "))
	}
}

func emitCanonTrack(lines *[]string, tr TrackJson) {
	boring := len(tr.Items) == 0 && (tr.Name == nil || *tr.Name == "") && !tr.Muted && tr.Height == 1.0
	if boring {
		return
	}
	*lines = append(*lines, "", fmt.Sprintf("track %d:", tr.Index))
	emitKv(lines, "  name", tr.Name, 0)
	emitKv(lines, "  color", fmtColorCanon(tr.Color), 0)
	emitKv(lines, "  height", fmtFloatPtr(&tr.Height, 3), 0)
	if tr.Muted {
		*lines = append(*lines, "  muted: true")
	}
	for _, it := range tr.Items {
		parts := []string{fmt.Sprintf("pos=%d", it.Position), fmt.Sprintf("len=%d", it.Length)}
		if it.PatternIid != nil {
			parts = append(parts, fmt.Sprintf("pattern=%d", *it.PatternIid))
		}
		if it.ChannelIid != nil {
			parts = append(parts, fmt.Sprintf("channel=%d", *it.ChannelIid))
		}
		if it.Muted {
			parts = append(parts, "muted")
		}
		*lines = append(*lines, "  - clip "+strings.Join(parts, " "))
	}
}
