package flp

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// RenderSummaryOptions configures RenderSummary.
type RenderSummaryOptions struct {
	Title   string
	Verbose bool
}

const (
	markerAdded    = "+"
	markerRemoved  = "-"
	markerModified = "~"
)

// RenderSummary produces a multi-line text summary of a DiffResult.
func RenderSummary(result DiffResult, opts RenderSummaryOptions) string {
	lines := []string{}
	if opts.Title != "" {
		header := "FLP Diff: " + opts.Title
		lines = append(lines, header)
		width := len(utf16.Encode([]rune(header)))
		if width > 60 {
			width = 60
		}
		lines = append(lines, strings.Repeat("─", width))
	}
	lines = append(lines, "Summary: "+result.Summary.HumanLabel)
	if DiffResultIsIdentical(result) {
		return strings.Join(lines, "\n")
	}
	if len(result.MetadataChanges) > 0 {
		lines = emitMetadata(lines, result.MetadataChanges)
	}
	if len(result.ChannelChanges) > 0 {
		lines = emitChannels(lines, result.ChannelChanges)
	}
	if len(result.PatternChanges) > 0 {
		lines = emitPatterns(lines, result.PatternChanges)
	}
	if !IsMixerDiffEmpty(result.MixerChanges) {
		lines = emitMixer(lines, result.MixerChanges)
	}
	if len(result.ArrangementChanges) > 0 {
		lines = emitArrangements(lines, result.ArrangementChanges, opts.Verbose)
	}
	if len(result.OpaqueChanges) > 0 {
		lines = emitOpaques(lines, result.OpaqueChanges)
	}
	return strings.Join(lines, "\n")
}

func markerFor(kind string) string {
	switch kind {
	case "added":
		return markerAdded
	case "removed":
		return markerRemoved
	}
	return markerModified
}

func sectionHeader(lines []string, title string) []string {
	return append(lines, "", title+":")
}

func emitMetadata(lines []string, changes []Change) []string {
	lines = sectionHeader(lines, "Metadata")
	for _, c := range changes {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(c.Kind)), c.HumanLabel))
	}
	return lines
}

func emitChannels(lines []string, diffs []ChannelDiff) []string {
	lines = sectionHeader(lines, "Channels")
	for _, d := range diffs {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(d.Kind)), d.HumanLabel))
		for _, c := range d.Changes {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(c.Kind)), c.HumanLabel))
		}
		for _, ac := range d.AutomationChanges {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(ac.Kind)), ac.HumanLabel))
		}
	}
	return lines
}

func emitPatterns(lines []string, diffs []PatternDiff) []string {
	lines = sectionHeader(lines, "Patterns")
	for _, d := range diffs {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(d.Kind)), d.HumanLabel))
		for _, c := range d.Changes {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(c.Kind)), c.HumanLabel))
		}
		if len(d.NoteChanges) > 0 {
			lines = emitNoteChanges(lines, d.NoteChanges)
		}
		for _, ac := range d.ControllerChanges {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(ac.Kind)), ac.HumanLabel))
		}
	}
	return lines
}

const (
	maxVerbatimNotes = 10
	examplesPerKind  = 3
)

func emitNoteChanges(lines []string, notes []NoteChange) []string {
	if len(notes) <= maxVerbatimNotes {
		for _, nc := range notes {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(nc.Kind)), nc.HumanLabel))
		}
		return lines
	}
	byKind := map[NoteChangeKind][]NoteChange{}
	for _, nc := range notes {
		byKind[nc.Kind] = append(byKind[nc.Kind], nc)
	}
	for _, kind := range []NoteChangeKind{NoteModified, NoteMoved, NoteRemoved, NoteAdded} {
		bucket := byKind[kind]
		if len(bucket) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("      %s %d notes %s", markerFor(string(kind)), len(bucket), kind))
		for i, nc := range bucket {
			if i >= examplesPerKind {
				break
			}
			lines = append(lines, "          · "+nc.HumanLabel)
		}
		if len(bucket) > examplesPerKind {
			lines = append(lines, fmt.Sprintf("          · … and %d more", len(bucket)-examplesPerKind))
		}
	}
	return lines
}

func emitMixer(lines []string, mixer MixerDiff) []string {
	lines = sectionHeader(lines, "Mixer")
	for _, d := range mixer.Inserts {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(d.Kind)), d.HumanLabel))
		for _, c := range d.Changes {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(c.Kind)), c.HumanLabel))
		}
	}
	for _, c := range mixer.Changes {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(c.Kind)), c.HumanLabel))
	}
	return lines
}

func emitArrangements(lines []string, diffs []ArrangementDiff, verbose bool) []string {
	lines = sectionHeader(lines, "Arrangements")
	for _, d := range diffs {
		lines = append(lines, fmt.Sprintf("  %s %s", markerFor(string(d.Kind)), d.HumanLabel))
		for _, c := range d.Changes {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(c.Kind)), c.HumanLabel))
		}
		for _, td := range d.TrackChanges {
			lines = append(lines, fmt.Sprintf("      %s %s", markerFor(string(td.Kind)), td.HumanLabel))
			lines = emitTrackChanges(lines, td, verbose)
		}
	}
	return lines
}

func emitTrackChanges(lines []string, td TrackDiff, verbose bool) []string {
	hasGroups := len(td.ClipMoveGroups) > 0 || len(td.ClipBulkGroups) > 0 || len(td.ClipModifyGroups) > 0
	if verbose || !hasGroups {
		for _, c := range td.Changes {
			lines = append(lines, fmt.Sprintf("          %s %s", markerFor(string(c.Kind)), c.HumanLabel))
		}
		return lines
	}
	covered := map[string]bool{}
	for _, g := range td.ClipMoveGroups {
		for _, p := range g.ChangePaths {
			covered[p] = true
		}
	}
	for _, g := range td.ClipBulkGroups {
		for _, p := range g.ChangePaths {
			covered[p] = true
		}
	}
	for _, g := range td.ClipModifyGroups {
		for _, p := range g.ChangePaths {
			covered[p] = true
		}
	}
	for _, g := range td.ClipMoveGroups {
		lines = append(lines, fmt.Sprintf("          %s %s", markerModified, g.HumanLabel))
	}
	for _, g := range td.ClipBulkGroups {
		m := markerRemoved
		if g.Kind == "added" {
			m = markerAdded
		}
		lines = append(lines, fmt.Sprintf("          %s %s", m, g.HumanLabel))
	}
	for _, g := range td.ClipModifyGroups {
		lines = append(lines, fmt.Sprintf("          %s %s", markerModified, g.HumanLabel))
	}
	for _, c := range td.Changes {
		if covered[c.Path] {
			continue
		}
		lines = append(lines, fmt.Sprintf("          %s %s", markerFor(string(c.Kind)), c.HumanLabel))
	}
	return lines
}

func emitOpaques(lines []string, opaques []OpaqueChange) []string {
	lines = sectionHeader(lines, "Opaque blobs")
	for _, oc := range opaques {
		lines = append(lines, fmt.Sprintf("  %s %s", markerModified, oc.HumanLabel))
	}
	return lines
}
