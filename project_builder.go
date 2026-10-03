package flp

import (
	"strconv"
	"strings"
)

const (
	opNewChannel      = 0x40
	opChannelType     = 0x15
	opChannelEnabled  = 0x00
	opChannelPingPong = 0x14
	opChannelLocked   = 0x20
	opChannelZipped   = 0x0f
	opChannelRoutedTo = 0x16
	opChannelAutom    = 0xea
	opRemoteCtrl      = 0xe3
	opSamplePath      = 0xc4
	opPluginInternal  = 0xc9
	opPluginColor     = 0x80
	opChannelLevels   = 0xdb
	opPluginState     = 0xd5
	opName            = 0xcb
	opChannelName     = 0xc0
	opNewSlot         = 0x62
	opInsertEnd       = 0x93
	opInsertName      = 0xcc
	opInsertColor     = 0x95
	opInsertIcon      = 0x5f
	opInsertInput     = 0x9a
	opInsertFlags     = 0xec
	opInsertRouting   = 0xe7
	opMixerParams     = 0xe1
	mpSlotEnabled     = 0
	mpSlotMix         = 1
	mpInsertVolume    = 192
	mpInsertPan       = 193
	mpInsertStereoSep = 194
	routingUnset      = 0xffffffff
	opPatternNew      = 0x41
	opPatternName     = 0xc1
	opPatternNotes    = 0xe0
	opPatternCtrls    = 0xdf
	opPatternColor    = 0x96
	opPatternLength   = 0xa4
	opPatternLooped   = 0x1a
	opArrangementNew  = 0x63
	opArrangementName = 0xf1
	opTrackData       = 0xee
	opTrackName       = 0xef
	opPlaylist        = 0xe9
	opTMPosition      = 0x94
	opTMNumerator     = 0x21
	opTMDenominator   = 0x22
	opTMName          = 0xcd

	opProjLoop      = 0x09
	opProjShowInfo  = 0x0a
	opProjVolume    = 0x0c
	opProjTimeNum   = 0x11
	opProjTimeDenom = 0x12
	opProjPanLaw    = 0x17
	opProjPitch     = 0x50
	opProjTitle     = 0xc2
	opProjComments  = 0xc3
	opProjURL       = 0xc5
	opProjRTF       = 0xc6
	opProjVersion   = 0xc7
	opProjDataPath  = 0xca
	opProjGenre     = 0xce
	opProjArtists   = 0xcf
	opProjBuild     = 0x9f
	opProjTimestamp = 0xed

	playlistMaxTrackIdx = 499
	playlistPatternBase = 20480
)

func parseFlVersionAscii(value string) *FLVersion {
	parts := strings.Split(value, ".")
	if len(parts) < 3 {
		return nil
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			nums[i] = 0 // JS Number("") === 0
			continue
		}
		n, err := strconv.Atoi(t)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	v := &FLVersion{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if len(nums) >= 4 {
		v.Build = Ptr(nums[3])
	}
	return v
}

func isLegacyText(meta *ProjectMetadata) bool {
	if meta == nil || meta.Version == nil {
		return false
	}
	v := meta.Version
	return v.Major < 11 || (v.Major == 11 && v.Minor < 5)
}

func clipRecordSizeFor(meta *ProjectMetadata) int {
	if meta == nil || meta.Version == nil {
		return 0
	}
	v := meta.Version
	if v.Major >= 25 {
		return 80
	}
	if v.Major >= 21 {
		return 60
	}
	return 32
}

func decodeTextEvent(payload []byte, legacy bool) string {
	if legacy {
		return decodeUtf8Bytes(payload)
	}
	return decodeUtf16LeBytes(payload)
}

// collectInsertRouting concatenates every 0xE7 payload in stream order.
func collectInsertRouting(events []FLPEvent) []bool {
	out := []bool{}
	for _, ev := range events {
		if ev.Opcode == opInsertRouting && ev.Kind == KindBlob {
			out = append(out, decodeInsertRouting(ev.Payload)...)
		}
	}
	return out
}

func buildMetadata(events []FLPEvent) ProjectMetadata {
	out := ProjectMetadata{}
	var flBuild *uint32

	for _, ev := range events {
		if ev.Opcode == opProjVersion && ev.Kind == KindBlob {
			s := decodeUtf8Bytes(ev.Payload)
			if v := parseFlVersionAscii(s); v != nil {
				out.Version = v
				break
			}
		}
	}
	legacy := isLegacyText(&out)
	decodeText := func(p []byte) string { return decodeTextEvent(p, legacy) }

	for _, ev := range events {
		switch {
		case ev.Opcode == opProjTitle && ev.Kind == KindBlob && out.Title == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.Title = &s
			}
		case ev.Opcode == opProjArtists && ev.Kind == KindBlob && out.Artists == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.Artists = &s
			}
		case ev.Opcode == opProjGenre && ev.Kind == KindBlob && out.Genre == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.Genre = &s
			}
		case ev.Opcode == opProjURL && ev.Kind == KindBlob && out.URL == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.URL = &s
			}
		case ev.Opcode == opProjDataPath && ev.Kind == KindBlob && out.DataPath == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.DataPath = &s
			}
		case ev.Opcode == opProjComments && ev.Kind == KindBlob && out.Comments == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.Comments = &s
			}
		case ev.Opcode == opProjRTF && ev.Kind == KindBlob && out.Comments == nil:
			if s := decodeText(ev.Payload); len(s) > 0 {
				out.Comments = &s
			}
		case ev.Opcode == opProjBuild && ev.Kind == KindU32:
			v := ev.Value
			flBuild = &v
		case ev.Opcode == opProjLoop && ev.Kind == KindU8 && out.Looped == nil:
			out.Looped = Ptr(ev.Value != 0)
		case ev.Opcode == opProjShowInfo && ev.Kind == KindU8 && out.ShowInfo == nil:
			out.ShowInfo = Ptr(ev.Value != 0)
		case ev.Opcode == opProjPitch && ev.Kind == KindU16 && out.MainPitch == nil:
			v := int(ev.Value)
			if v > 0x7fff {
				v -= 0x10000
			}
			out.MainPitch = &v
		case ev.Opcode == opProjVolume && ev.Kind == KindU8 && out.MainVolume == nil:
			out.MainVolume = Ptr(int(ev.Value))
		case ev.Opcode == opProjPanLaw && ev.Kind == KindU8 && out.PanLaw == nil:
			out.PanLaw = Ptr(int(ev.Value))
		case ev.Opcode == opProjTimeNum && ev.Kind == KindU8 && out.TimeSignatureNumerator == nil:
			out.TimeSignatureNumerator = Ptr(int(ev.Value))
		case ev.Opcode == opProjTimeDenom && ev.Kind == KindU8 && out.TimeSignatureDenominator == nil:
			out.TimeSignatureDenominator = Ptr(int(ev.Value))
		case ev.Opcode == opProjTimestamp && ev.Kind == KindBlob && out.CreatedOn == nil:
			if t, spent, ok := decodeTimestamp(ev.Payload); ok {
				tt := t
				out.CreatedOn = &tt
				out.TimeSpentSeconds = Ptr(spent)
			}
		}
	}
	if out.Version != nil && out.Version.Build == nil && flBuild != nil {
		nv := *out.Version
		nv.Build = Ptr(int(*flBuild))
		out.Version = &nv
	}
	return out
}

func buildChannels(events []FLPEvent, metadata *ProjectMetadata) []Channel {
	legacy := isLegacyText(metadata)
	chans := []*Channel{}
	var current *Channel
	scope := "outside"
	sawPluginEvent := map[int]bool{}

	for _, ev := range events {
		if ev.Opcode == opNewChannel && ev.Kind == KindU16 {
			current = &Channel{Iid: int(ev.Value), Kind: ChannelUnknown}
			chans = append(chans, current)
			scope = "channel"
			continue
		}
		if ev.Opcode == opNewSlot {
			scope = "slot"
			continue
		}
		if ev.Opcode == opInsertEnd || ev.Opcode == opInsertFlags {
			scope = "outside"
			continue
		}
		if scope != "channel" || current == nil {
			continue
		}
		switch {
		case ev.Opcode == opChannelType && ev.Kind == KindU8:
			current.Kind = classifyChannelKind(int(ev.Value))
		case ev.Opcode == opChannelEnabled && ev.Kind == KindU8 && current.Enabled == nil:
			current.Enabled = Ptr(ev.Value != 0)
		case ev.Opcode == opChannelPingPong && ev.Kind == KindU8 && current.PingPongLoop == nil:
			current.PingPongLoop = Ptr(ev.Value != 0)
		case ev.Opcode == opChannelLocked && ev.Kind == KindU8 && current.Locked == nil:
			current.Locked = Ptr(ev.Value != 0)
		case ev.Opcode == opChannelZipped && ev.Kind == KindU8 && current.Zipped == nil:
			current.Zipped = Ptr(ev.Value != 0)
		case ev.Opcode == opChannelRoutedTo && ev.Kind == KindU8 && current.TargetInsert == nil:
			v := int(ev.Value)
			if v > 127 {
				v -= 256
			}
			current.TargetInsert = &v
		case ev.Opcode == opChannelAutom && ev.Kind == KindBlob && current.AutomationPoints == nil:
			current.AutomationPoints = decodeAutomationPoints(ev.Payload)
		case ev.Opcode == opPluginColor && ev.Kind == KindU32 && current.Color == nil:
			c := unpackRGBA(ev.Value)
			current.Color = &c
		case ev.Opcode == opChannelLevels && ev.Kind == KindBlob && current.Levels == nil:
			if lv := decodeLevels(ev.Payload); lv != nil {
				current.Levels = lv
			}
		case ev.Opcode == opSamplePath && ev.Kind == KindBlob:
			s := decodeTextEvent(ev.Payload, legacy)
			current.SamplePath = &s
		case ev.Opcode == opPluginInternal && ev.Kind == KindBlob && current.Plugin == nil:
			sawPluginEvent[current.Iid] = true
			internal := decodeTextEvent(ev.Payload, legacy)
			if len(internal) > 0 {
				current.Plugin = &ChannelPlugin{InternalName: internal}
			}
		case ev.Opcode == opPluginState && ev.Kind == KindBlob && current.Plugin != nil &&
			current.Plugin.InternalName == "Fruity Wrapper" && current.Plugin.Name == nil:
			info := decodeVSTWrapper(ev.Payload)
			if info.Name != nil {
				current.Plugin.Name = info.Name
			}
			if info.Vendor != nil {
				current.Plugin.Vendor = info.Vendor
			}
		case ev.Opcode == opName && ev.Kind == KindBlob:
			s := decodeTextEvent(ev.Payload, legacy)
			current.Name = &s
		case ev.Opcode == opChannelName && ev.Kind == KindBlob && current.Name == nil:
			s := decodeTextEvent(ev.Payload, legacy)
			current.Name = &s
		}
	}

	for _, ch := range chans {
		if ch.Kind == ChannelInstrument && ch.SamplePath != nil && sawPluginEvent[ch.Iid] && ch.Plugin == nil {
			ch.Kind = ChannelSampler
		}
	}

	boundaries := []int{}
	for i, ev := range events {
		if ev.Opcode == opNewChannel && ev.Kind == KindU16 {
			boundaries = append(boundaries, i)
		}
	}
	for c, ch := range chans {
		start := boundaries[c] + 1
		end := len(events)
		if c+1 < len(boundaries) {
			end = boundaries[c+1]
		}
		for i := start; i < end; i++ {
			ev := events[i]
			if ev.Opcode == opName && ev.Kind == KindBlob {
				s := decodeTextEvent(ev.Payload, legacy)
				ch.Name = &s
				break
			}
		}
	}

	byIid := map[int]*Channel{}
	for _, c := range chans {
		byIid[c.Iid] = c
	}
	for _, ev := range events {
		if ev.Opcode != opRemoteCtrl || ev.Kind != KindBlob {
			continue
		}
		if len(ev.Payload) < 12 {
			continue
		}
		sourceIid := int(le.Uint16(ev.Payload[2:]))
		ch := byIid[sourceIid]
		if ch == nil || ch.Kind != ChannelAutomation || ch.AutomationTarget != nil {
			continue
		}
		paramData := int(le.Uint16(ev.Payload[8:]))
		rawDest := int(le.Uint16(ev.Payload[10:]))
		ch.AutomationTarget = &AutomationTarget{
			Kind:           "unknown",
			ParamID:        paramData & 0x7fff,
			IsVstParam:     paramData&0x8000 != 0,
			RawDestination: rawDest,
		}
	}

	iids := map[int]bool{}
	for _, c := range chans {
		iids[c.Iid] = true
	}
	const mixerSlotMarker = 0x2000
	for _, ch := range chans {
		t := ch.AutomationTarget
		if t == nil {
			continue
		}
		if t.RawDestination&mixerSlotMarker != 0 {
			t.Kind = "mixer_slot"
			t.TargetInsertIndex = Ptr((t.RawDestination >> 6) & 0x7f)
			t.TargetSlotIndex = Ptr(t.RawDestination & 0x3f)
		} else if iids[t.RawDestination] {
			t.Kind = "channel"
			t.TargetChannelIid = Ptr(t.RawDestination)
		}
	}

	out := make([]Channel, len(chans))
	for i, c := range chans {
		out[i] = *c
	}
	return out
}

func buildMixerInserts(events []FLPEvent, metadata *ProjectMetadata) []MixerInsert {
	legacy := isLegacyText(metadata)
	inserts := []*MixerInsert{}
	var mixerParams []MixerParamRecord
	pendingInsert := &MixerInsert{Index: 0, Slots: []MixerSlot{}}
	pendingSlot := &MixerSlot{Index: 0}
	firstSlotSeen := false
	inMixerSection := false

	for _, ev := range events {
		switch {
		case ev.Opcode == opInsertEnd && ev.Kind == KindU32:
			inMixerSection = true
			if ev.Value != routingUnset && int(ev.Value) != pendingInsert.Index {
				pendingInsert.Output = Ptr(ev.Value)
			}
			pendingInsert.Slots = append(pendingInsert.Slots, *pendingSlot)
			inserts = append(inserts, pendingInsert)
			pendingInsert = &MixerInsert{Index: len(inserts), Slots: []MixerSlot{}}
			pendingSlot = &MixerSlot{Index: 0}
			firstSlotSeen = false
		case ev.Opcode == opNewSlot && ev.Kind == KindU16:
			inMixerSection = true
			if !firstSlotSeen {
				firstSlotSeen = true
				pendingSlot.Index = int(ev.Value)
			} else {
				pendingInsert.Slots = append(pendingInsert.Slots, *pendingSlot)
				pendingSlot = &MixerSlot{Index: int(ev.Value)}
			}
		case ev.Opcode == opInsertName && ev.Kind == KindBlob && pendingInsert.Name == nil:
			inMixerSection = true
			s := decodeTextEvent(ev.Payload, legacy)
			pendingInsert.Name = &s
		case ev.Opcode == opInsertColor && ev.Kind == KindU32 && pendingInsert.Color == nil:
			inMixerSection = true
			c := unpackRGBA(ev.Value)
			pendingInsert.Color = &c
		case ev.Opcode == opInsertIcon && ev.Kind == KindU16 && pendingInsert.Icon == nil:
			inMixerSection = true
			pendingInsert.Icon = Ptr(int(ev.Value))
		case ev.Opcode == opInsertInput && ev.Kind == KindU32 && pendingInsert.Input == nil:
			inMixerSection = true
			if ev.Value != routingUnset {
				pendingInsert.Input = Ptr(ev.Value)
			}
		case ev.Opcode == opInsertFlags && ev.Kind == KindBlob && pendingInsert.Flags == nil:
			inMixerSection = true
			if f := decodeInsertFlags(ev.Payload); f != nil {
				pendingInsert.Flags = f
			}
		case ev.Opcode == opName && ev.Kind == KindBlob && inMixerSection && pendingSlot.PluginName == nil:
			s := decodeTextEvent(ev.Payload, legacy)
			pendingSlot.PluginName = &s
		case ev.Opcode == opPluginInternal && ev.Kind == KindBlob && inMixerSection && pendingSlot.InternalName == nil:
			n := decodeTextEvent(ev.Payload, legacy)
			if len(n) > 0 {
				pendingSlot.InternalName = &n
			}
		case ev.Opcode == opPluginState && ev.Kind == KindBlob && inMixerSection:
			pendingSlot.HasPlugin = Ptr(true)
			if pendingSlot.InternalName != nil && *pendingSlot.InternalName == "Fruity Wrapper" && pendingSlot.PluginVstName == nil {
				info := decodeVSTWrapper(ev.Payload)
				if info.Name != nil {
					pendingSlot.PluginVstName = info.Name
				}
				if info.Vendor != nil {
					pendingSlot.PluginVendor = info.Vendor
				}
			}
		case ev.Opcode == opMixerParams && ev.Kind == KindBlob:
			mixerParams = decodeMixerParams(ev.Payload)
		}
	}

	for _, rec := range mixerParams {
		if rec.InsertIdx < 0 || rec.InsertIdx >= len(inserts) {
			continue
		}
		ins := inserts[rec.InsertIdx]
		switch rec.ID {
		case mpInsertVolume:
			ins.Volume = Ptr(rec.Msg)
		case mpInsertPan:
			ins.Pan = Ptr(rec.Msg)
		case mpInsertStereoSep:
			ins.StereoSeparation = Ptr(rec.Msg)
		case mpSlotEnabled:
			if rec.SlotIdx < len(ins.Slots) {
				ins.Slots[rec.SlotIdx].Enabled = Ptr(rec.Msg != 0)
			}
		case mpSlotMix:
			if rec.SlotIdx < len(ins.Slots) {
				ins.Slots[rec.SlotIdx].Mix = Ptr(rec.Msg)
			}
		}
	}

	out := make([]MixerInsert, len(inserts))
	for i, ins := range inserts {
		out[i] = *ins
	}
	return out
}

func buildPatterns(events []FLPEvent, metadata *ProjectMetadata) []Pattern {
	legacy := isLegacyText(metadata)
	byID := map[int]*Pattern{}
	order := []int{}
	currentID := -1

	for _, ev := range events {
		switch {
		case ev.Opcode == opPatternNew && ev.Kind == KindU16:
			currentID = int(ev.Value)
			if _, ok := byID[currentID]; !ok {
				byID[currentID] = &Pattern{ID: currentID, Notes: []Note{}, Controllers: []Controller{}}
				order = append(order, currentID)
			}
		case currentID < 0:
			continue
		case ev.Opcode == opPatternName && ev.Kind == KindBlob:
			if p := byID[currentID]; p != nil && p.Name == nil {
				s := decodeTextEvent(ev.Payload, legacy)
				p.Name = &s
			}
		case ev.Opcode == opPatternNotes && ev.Kind == KindBlob:
			if p := byID[currentID]; p != nil {
				p.Notes = append(p.Notes, decodeNotes(ev.Payload)...)
			}
		case ev.Opcode == opPatternCtrls && ev.Kind == KindBlob:
			if p := byID[currentID]; p != nil {
				p.Controllers = append(p.Controllers, decodeControllers(ev.Payload)...)
			}
		case ev.Opcode == opPatternColor && ev.Kind == KindU32:
			if p := byID[currentID]; p != nil && p.Color == nil {
				c := unpackRGBA(ev.Value)
				p.Color = &c
			}
		case ev.Opcode == opPatternLength && ev.Kind == KindU32:
			if p := byID[currentID]; p != nil && p.Length == nil {
				p.Length = Ptr(ev.Value)
			}
		case ev.Opcode == opPatternLooped && ev.Kind == KindU8:
			if p := byID[currentID]; p != nil && p.Looped == nil {
				p.Looped = Ptr(ev.Value != 0)
			}
		}
	}
	out := make([]Pattern, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func buildArrangements(events []FLPEvent, channels []Channel, patterns []Pattern, metadata *ProjectMetadata) []Arrangement {
	legacy := isLegacyText(metadata)
	channelIids := map[int]bool{}
	for _, c := range channels {
		channelIids[c.Iid] = true
	}
	patternIDs := map[int]bool{}
	for _, p := range patterns {
		patternIDs[p.ID] = true
	}
	clipRecordSize := clipRecordSizeFor(metadata)

	keepClip := func(c Clip) bool {
		if c.TrackRvidx > playlistMaxTrackIdx {
			return false
		}
		if c.ItemIndex <= playlistPatternBase {
			return channelIids[c.ItemIndex]
		}
		return patternIDs[c.ItemIndex-playlistPatternBase]
	}

	arrangements := []*Arrangement{}
	var current *Arrangement
	var pendingMarker *TimeMarker
	flush := func() {
		if pendingMarker != nil && current != nil {
			current.TimeMarkers = append(current.TimeMarkers, *pendingMarker)
		}
		pendingMarker = nil
	}

	for _, ev := range events {
		if ev.Opcode == opArrangementNew && ev.Kind == KindU16 {
			flush()
			current = &Arrangement{ID: int(ev.Value), Tracks: []Track{}, Clips: []Clip{}, TimeMarkers: []TimeMarker{}}
			arrangements = append(arrangements, current)
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case ev.Opcode == opArrangementName && ev.Kind == KindBlob && current.Name == nil:
			s := decodeTextEvent(ev.Payload, legacy)
			current.Name = &s
		case ev.Opcode == opTrackData && ev.Kind == KindBlob:
			current.Tracks = append(current.Tracks, decodeTrackData(ev.Payload, len(current.Tracks)))
		case ev.Opcode == opTrackName && ev.Kind == KindBlob:
			if n := len(current.Tracks); n > 0 {
				s := decodeTextEvent(ev.Payload, legacy)
				current.Tracks[n-1].Name = &s
			}
		case ev.Opcode == opPlaylist && ev.Kind == KindBlob:
			for _, clip := range decodeClips(ev.Payload, clipRecordSize) {
				if keepClip(clip) {
					current.Clips = append(current.Clips, clip)
				}
			}
		case ev.Opcode == opTMPosition && ev.Kind == KindU32:
			flush()
			kind, pos := decodeTimeMarkerPosition(ev.Value)
			pendingMarker = &TimeMarker{Kind: kind, Position: pos}
		case ev.Opcode == opTMName && ev.Kind == KindBlob && pendingMarker != nil:
			s := decodeTextEvent(ev.Payload, legacy)
			pendingMarker.Name = &s
		case ev.Opcode == opTMNumerator && ev.Kind == KindU8 && pendingMarker != nil:
			pendingMarker.Numerator = Ptr(int(ev.Value))
		case ev.Opcode == opTMDenominator && ev.Kind == KindU8 && pendingMarker != nil:
			pendingMarker.Denominator = Ptr(int(ev.Value))
		}
	}
	flush()

	out := make([]Arrangement, len(arrangements))
	for i, a := range arrangements {
		out[i] = *a
	}
	return out
}
