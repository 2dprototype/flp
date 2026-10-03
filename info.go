package flp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func infoActiveInserts(inserts []MixerInsert) []MixerInsert {
	out := []MixerInsert{}
	for _, i := range inserts {
		if len(i.Slots) > 0 || i.Name != nil {
			out = append(out, i)
		}
	}
	return out
}

func infoEffectSlotCount(inserts []MixerInsert) int {
	n := 0
	for _, i := range inserts {
		for _, s := range i.Slots {
			if (s.HasPlugin != nil && *s.HasPlugin) || (s.PluginName != nil && *s.PluginName != "") {
				n++
			}
		}
	}
	return n
}

func collectPluginNames(p *FLPProject) []string {
	seen := map[string]bool{}
	order := []string{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		order = append(order, n)
	}
	for _, ch := range p.Channels {
		if ch.Plugin != nil {
			name := ch.Plugin.InternalName
			if ch.Plugin.Name != nil {
				name = *ch.Plugin.Name
			}
			add(name)
		}
	}
	for _, ins := range p.Inserts {
		for _, s := range ins.Slots {
			var name string
			switch {
			case s.PluginVstName != nil:
				name = *s.PluginVstName
			case s.PluginName != nil:
				name = *s.PluginName
			case s.InternalName != nil:
				name = *s.InternalName
			}
			add(name)
		}
	}
	return order
}

func collectSamplePaths(p *FLPProject) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ch := range p.Channels {
		if ch.SamplePath != nil && *ch.SamplePath != "" && !seen[*ch.SamplePath] {
			seen[*ch.SamplePath] = true
			out = append(out, *ch.SamplePath)
		}
	}
	return out
}

func fmtInfoTimeSig(num, denom *int) string {
	if num == nil || denom == nil {
		return "?"
	}
	return fmt.Sprintf("%d/%d", *num, *denom)
}

func fmtChannelKinds(counts map[string]int) string {
	if len(counts) == 0 {
		return "0"
	}
	total := 0
	keys := make([]string, 0, len(counts))
	for k, n := range counts {
		total += n
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		n := counts[k]
		s := ""
		if n != 1 {
			s = "s"
		}
		parts[i] = fmt.Sprintf("%d %s%s", n, k, s)
	}
	return fmt.Sprintf("%d (%s)", total, strings.Join(parts, ", "))
}

func fmtList(values []string, maxItems int) string {
	if len(values) == 0 {
		return "(none)"
	}
	if len(values) <= maxItems {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s, … and %d more", strings.Join(values[:maxItems], ", "), len(values)-maxItems)
}

func fmtInfoVersion(v *FLVersion) string {
	if v == nil {
		return "?"
	}
	parts := []string{fmt.Sprint(v.Major), fmt.Sprint(v.Minor), fmt.Sprint(v.Patch)}
	if v.Build != nil {
		parts = append(parts, fmt.Sprint(*v.Build))
	}
	return strings.Join(parts, ".")
}

// basenameAny returns the last path component (handles both / and \ is NOT done; mirrors node:path.basename on POSIX).
func basenameAny(p string) string {
	return filepath.Base(p)
}

// RenderInfo renders the human-readable inspection report.
func RenderInfo(p *FLPProject, filePath string) string {
	md := p.Metadata
	counts := map[string]int{}
	for _, ch := range p.Channels {
		counts[string(ch.Kind)]++
	}
	active := infoActiveInserts(p.Inserts)
	slots := infoEffectSlotCount(p.Inserts)
	pluginNames := collectPluginNames(p)
	sampleNames := []string{}
	for _, sp := range collectSamplePaths(p) {
		i := strings.LastIndex(sp, "/")
		if j := strings.LastIndex(sp, "\\"); j > i {
			i = j
		}
		if i < 0 {
			sampleNames = append(sampleNames, sp)
		} else {
			sampleNames = append(sampleNames, sp[i+1:])
		}
	}
	tracksTotal, clipsTotal := 0, 0
	for _, a := range p.Arrangements {
		tracksTotal += len(a.Tracks)
		clipsTotal += len(a.Clips)
	}
	tempo := 0.0
	if t := GetTempo(p); t != nil {
		tempo = *t
	}
	lines := []string{
		"File: " + basenameAny(filePath),
		fmt.Sprintf("FL Studio %s | %s BPM | %s | PPQ %d", fmtInfoVersion(md.Version), jsToFixed(tempo, 1),
			fmtInfoTimeSig(md.TimeSignatureNumerator, md.TimeSignatureDenominator), p.Header.PPQ),
	}
	if md.Title != nil && *md.Title != "" {
		lines = append(lines, "Title: "+*md.Title)
	}
	if md.Artists != nil && *md.Artists != "" {
		lines = append(lines, "Artists: "+*md.Artists)
	}
	if md.Genre != nil && *md.Genre != "" {
		lines = append(lines, "Genre: "+*md.Genre)
	}
	lines = append(lines,
		"Channels: "+fmtChannelKinds(counts),
		fmt.Sprintf("Patterns: %d", len(p.Patterns)),
		fmt.Sprintf("Mixer: %d active inserts, %d effect slots", len(active), slots),
		fmt.Sprintf("Arrangements: %d (%d tracks, %d clips)", len(p.Arrangements), tracksTotal, clipsTotal),
		"Plugins: "+fmtList(pluginNames, 8),
		"Samples: "+fmtList(sampleNames, 8),
	)
	return strings.Join(lines, "\n")
}
