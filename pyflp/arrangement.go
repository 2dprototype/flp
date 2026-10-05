package pyflp

import (
	"encoding/binary"
	"fmt"
)

func trackProp(name string, kind PropKind, doc, key string) *PropSpec {
	return stProp(name, kind, doc, IDTrackData, key)
}

var trackSpecs = []*PropSpec{
	customProp("color", KColor, "Defaults to #485156 (dark slate gray). Unlike channels and inserts, values below 20 are NOT ignored by FL Studio.",
		func(m *Model) (interface{}, bool) {
			v, ok := m.fieldValue(IDTrackData, "color")
			if !ok {
				return nil, false
			}
			n, _ := toInt64(v)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], uint32(n))
			return RGBAFromBytes(b[:]), true
		},
		func(m *Model, v interface{}) error {
			c, _ := v.(RGBA)
			n := binary.LittleEndian.Uint32(c.Bytes())
			return m.setFieldValue(IDTrackData, "color", int64(n))
		}),
	trackProp("content_locked", KBool, "Lock to content, defaults to false.", "content_locked"),
	trackProp("enabled", KBool, "Whether the track is enabled.", "enabled"),
	trackProp("grouped", KBool, "Whether grouped with the track above (index - 1) or not.", "grouped"),
	trackProp("height", KString, "Track height in FL's interface as a percentage, e.g. \"100%\". Linear.", "height"),
	trackProp("icon", KInt, "0 if not set, else an internal icon ID.", "icon"),
	trackProp("iid", KInt, "An integer in the range of 1 to Arrangements.max_tracks.", "iid"),
	trackProp("locked", KBool, "Whether the track is in a locked state.", "locked"),
	withEnum(trackProp("motion", KEnum, "Performance settings, defaults to Stay.", "motion"), TrackMotionEnum),
	evProp("name", KString, "Name of the track; absent if not set.", IDTrackName),
	withEnum(trackProp("position_sync", KEnum, "Performance settings, defaults to Off.", "position_sync"), TrackSyncEnum),
	withEnum(trackProp("press", KEnum, "Performance settings, defaults to Retrigger.", "press"), TrackPressEnum),
	trackProp("tolerant", KBool, "Performance settings, defaults to true.", "tolerant"),
	withEnum(trackProp("trigger_sync", KEnum, "Performance settings, defaults to FourBeats.", "trigger_sync"), TrackSyncEnum),
	trackProp("queued", KBool, "Performance settings, defaults to false.", "queued"),
}

var plItemSpecs = []*PropSpec{
	itProp("group", KInt, "0 for no group, else a group number for clips in the same group.", "group"),
	itProp("length", KInt, "PPQ-dependant quantity.", "length"),
	itProp("position", KInt, "PPQ-dependant quantity.", "position"),
	itProp("start_offset", KFloat, "Distance from the item's actual start.", "start_offset"),
	itProp("end_offset", KFloat, "Distance from the item's actual end.", "end_offset"),
	itProp("item_index", KInt, "Index of the channel, or of the pattern plus pattern_base.", "item_index"),
	itProp("pattern_base", KInt, "Always 20480; channels have an item_index <= this value.", "pattern_base"),
	itProp("item_flags", KInt, "Raw item flags; usually 64.", "item_flags"),
	itProp("track_rvidx", KInt, "Stored reversed i.e. Track 1 would be 499.", "track_rvidx"),
}

// PLItem is an item on the playlist of an arrangement: a channel (audio
// clip / automation) or a pattern block.
//
// Note: PyFLP also lists a `muted` property but no such field exists in its
// item structure, so it never worked and is not ported.
type PLItem struct {
	*Model
	// Channel is set for audio clips and automations, Pattern for patterns.
	Channel *Channel
	Pattern *Pattern
}

// IsPattern reports whether the item is a pattern block.
func (p *PLItem) IsPattern() bool {
	idx, _ := p.item.Int("item_index")
	base, _ := p.item.Int("pattern_base")
	return idx > base
}

// Offsets returns the (start, end) offsets of the item.
func (p *PLItem) Offsets() (float64, float64) {
	s, _ := p.Float("start_offset")
	e, _ := p.Float("end_offset")
	return s, e
}

// SetOffsets sets the (start, end) offsets of the item.
func (p *PLItem) SetOffsets(start, end float64) error {
	if err := p.Set("start_offset", start); err != nil {
		return err
	}
	return p.Set("end_offset", end)
}

// SetChannel links a channel item to channel (updates item_index).
func (p *PLItem) SetChannel(c *Channel) error {
	if c == nil {
		return fmt.Errorf("%w: nil channel", ErrFLP)
	}
	if err := p.Set("item_index", c.IID()); err != nil {
		return err
	}
	p.Channel = c
	return nil
}

// SetPattern links a pattern item to pattern (updates item_index).
func (p *PLItem) SetPattern(pt *Pattern) error {
	if pt == nil {
		return fmt.Errorf("%w: nil pattern", ErrFLP)
	}
	base, _ := p.item.Int("pattern_base")
	if err := p.Set("item_index", pt.IID()+base); err != nil {
		return err
	}
	p.Pattern = pt
	return nil
}

func (p *PLItem) String() string {
	pos, _ := p.Int("position")
	l, _ := p.Int("length")
	if p.Pattern != nil {
		name, _ := p.Pattern.Str("name")
		return fmt.Sprintf("PatternPLItem(pattern=%q, position=%d, length=%d)", name, pos, l)
	}
	if p.Channel != nil {
		return fmt.Sprintf("ChannelPLItem(channel=%q, position=%d, length=%d)", p.Channel.DisplayName(), pos, l)
	}
	return fmt.Sprintf("PLItem(position=%d, length=%d)", pos, l)
}

// Track is a track in an arrangement on which playlist items are arranged.
type Track struct {
	*Model
	items []*PLItem
}

// Items returns the playlist items placed on the track.
func (t *Track) Items() []*PLItem { return t.items }

// Len returns the number of playlist items.
func (t *Track) Len() int { return len(t.items) }

// IID returns the track's index (1-based).
func (t *Track) IID() int64 { v, _ := t.Int("iid"); return v }

func (t *Track) String() string {
	name, _ := t.Str("name")
	return fmt.Sprintf("Track(name=%q, iid=%d, %d items)", name, t.IID(), len(t.items))
}

var arrangementSpecs = []*PropSpec{
	evProp("iid", KInt, "A 1-based internal index.", IDArrangementNew),
	evProp("name", KString, "Name of the arrangement; defaults to Arrangement.", IDArrangementName),
}

// Arrangement contains the timemarkers and tracks of an arrangement.
type Arrangement struct{ *Model }

func (a *Arrangement) version() FLVersion { v, _ := a.kw["version"].(FLVersion); return v }

// IID is the 1-based internal index.
func (a *Arrangement) IID() int64 { v, _ := a.Int("iid"); return v }

// TimeMarkers returns the timemarkers of the arrangement.
func (a *Arrangement) TimeMarkers() []*TimeMarker { return timeMarkersOf(a.Events) }

var trackDivideIDs = []EventID{IDTrackName, IDTrackData}

// Tracks returns the tracks of the arrangement together with the playlist
// items placed on them.
func (a *Arrangement) Tracks() []*Track {
	maxIdx := int64(198)
	if a.version().AtLeast(NewFLVersion(12, 9, 1)) {
		maxIdx = 499
	}
	channels := map[int64]*Channel{}
	if rack, ok := a.kw["channels"].(*ChannelRack); ok {
		all, _ := rack.All()
		for _, c := range all {
			channels[c.IID()] = c
		}
	}
	patterns := map[int64]*Pattern{}
	if ps, ok := a.kw["patterns"].(*Patterns); ok {
		for _, p := range ps.All() {
			patterns[p.IID()] = p
		}
	}

	var plEvt *Event
	if e := a.Events.FirstOf(IDArrangementPlaylist); e != nil {
		plEvt = e
	}
	var plList []*Container
	if plEvt != nil {
		plList, _ = plEvt.List()
	}

	var out []*Track
	for trackIdx, ed := range a.Events.Divide(IDTrackData, trackDivideIDs...) {
		t := &Track{Model: newModel("Track", ed, trackSpecs)}
		for i, item := range plList {
			rv, _ := item.Int("track_rvidx")
			if maxIdx-rv != int64(trackIdx) {
				continue
			}
			it := &PLItem{Model: newItemModel("PLItem", item, i, plEvt, plItemSpecs)}
			idx, _ := item.Int("item_index")
			base, _ := item.Int("pattern_base")
			if idx <= base {
				it.Model.Kind = "ChannelPLItem"
				it.Channel = channels[idx]
			} else {
				it.Model.Kind = "PatternPLItem"
				it.Pattern = patterns[idx-base]
			}
			t.items = append(t.items, it)
		}
		out = append(out, t)
	}
	return out
}

func (a *Arrangement) String() string {
	name, _ := a.Str("name")
	return fmt.Sprintf("Arrangement(iid=%d, name=%q, %d timemarkers, %d tracks)",
		a.IID(), name, len(a.TimeMarkers()), len(a.Tracks()))
}

var timeSignatureSpecs = []*PropSpec{
	evProp("num", KInt, "Beats per bar in time division & numerator in time signature mode. 1-16, default 4.", IDArrangementsTimeSigNum),
	evProp("beat", KInt, "Steps per beat in time division & denominator in time signature mode. 1-16, default 4 (2, 4, 8 or 16 in signature mode).", IDArrangementsTimeSigBeat),
}

// Arrangements is the collection of arrangements and some related
// properties.
type Arrangements struct{ *Model }

func (a *Arrangements) version() FLVersion { v, _ := a.kw["version"].(FLVersion); return v }

// Len returns the number of arrangements; ErrNoModelsFound if there are none.
func (a *Arrangements) Len() (int, error) {
	if !a.Events.Contains(IDArrangementNew) {
		return 0, ErrNoModelsFound
	}
	return a.Events.Count(IDArrangementNew), nil
}

// All returns the arrangements found in the project.
func (a *Arrangements) All() ([]*Arrangement, error) {
	n, err := a.Len()
	if err != nil {
		return nil, err
	}
	arrNew := false
	sel := func(e *Event) Select {
		if e.id == IDArrangementNew {
			if arrNew {
				return Cut
			}
			arrNew = true
		}
		if InGroups(e.id, GroupArrangement, GroupTimeMarker, GroupTrack) {
			return Include
		}
		if e.id == IDArrangementsCurrent {
			return Cut // yields out the last arrangement
		}
		return Skip
	}
	var out []*Arrangement
	for _, ed := range a.Events.Subtrees(sel, n) {
		m := newModel("Arrangement", ed, arrangementSpecs)
		for k, v := range a.kw {
			m.kw[k] = v
		}
		out = append(out, &Arrangement{m})
	}
	return out, nil
}

// At returns the arrangement at a zero-based index.
func (a *Arrangements) At(i int) (*Arrangement, error) {
	all, err := a.All()
	if err != nil {
		return nil, err
	}
	if i < 0 || i >= len(all) {
		return nil, modelNotFound(i)
	}
	return all[i], nil
}

// ByName returns the arrangement with the name.
func (a *Arrangements) ByName(name string) (*Arrangement, error) {
	all, err := a.All()
	if err != nil {
		return nil, err
	}
	for _, x := range all {
		if n, ok := x.Str("name"); ok && n == name {
			return x, nil
		}
	}
	return nil, modelNotFound(name)
}

// Current returns the arrangement currently selected in FL's interface (nil
// when the project has no such event).
func (a *Arrangements) Current() (*Arrangement, error) {
	e := a.Events.FirstOf(IDArrangementsCurrent)
	if e == nil {
		return nil, nil
	}
	idx, _ := e.Int()
	all, err := a.All()
	if err != nil {
		return nil, err
	}
	if idx < 0 || int(idx) >= len(all) {
		return nil, modelNotFound(idx)
	}
	return all[idx], nil
}

// LoopPos returns the playlist loop start and end points (PPQ dependant).
// PLSelection is used by default while LoopPos is a fallback.
func (a *Arrangements) LoopPos() ([2]int64, bool) {
	if e := a.Events.FirstOf(IDArrangementsPLSelection); e != nil {
		c, _ := e.Container()
		s, ok1 := c.Int("start")
		en, ok2 := c.Int("end")
		if ok1 && ok2 {
			return [2]int64{s, en}, true
		}
		return [2]int64{}, false
	}
	if e := a.Events.FirstOf(IDArrangementsLoopPos); e != nil {
		p, ok := e.Value().([2]int64)
		return p, ok
	}
	return [2]int64{}, false
}

// SetLoopPos sets the playlist loop start and end points.
func (a *Arrangements) SetLoopPos(start, end int64) error {
	if e := a.Events.FirstOf(IDArrangementsPLSelection); e != nil {
		if err := e.SetField("start", start); err != nil {
			return err
		}
		return e.SetField("end", end)
	}
	if e := a.Events.FirstOf(IDArrangementsLoopPos); e != nil {
		return e.SetValue([2]int64{start, end})
	}
	return cannotSet(IDArrangementsPLSelection, IDArrangementsLoopPos)
}

// MaxTracks is 500 from FL Studio 12.9.1 on, 199 before.
func (a *Arrangements) MaxTracks() int {
	if a.version().AtLeast(NewFLVersion(12, 9, 1)) {
		return 500
	}
	return 199
}

// TimeSignature is the project time signature (also used by the playlist).
func (a *Arrangements) TimeSignature() *Model {
	return newModel("TimeSignature",
		a.Events.SubtreeIDs(IDArrangementsTimeSigNum, IDArrangementsTimeSigBeat), timeSignatureSpecs)
}

func (a *Arrangements) String() string {
	n, _ := a.Len()
	return fmt.Sprintf("%d arrangements", n)
}
