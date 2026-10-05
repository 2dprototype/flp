package pyflp

import (
	"fmt"
	"strconv"
	"strings"
)

var noteNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName returns a name like "C5" or "A#3" for a key 0-131.
func NoteName(key int) string {
	return noteNames[((key%12)+12)%12] + strconv.Itoa(key/12)
}

// ParseNoteName converts "C5" / "A#3" into a key number.
func ParseNoteName(s string) (int, error) {
	s = strings.TrimSpace(s)
	for i, name := range noteNames {
		// "C#" must be tried before "C": check the longer names first.
		if len(name) == 2 && strings.HasPrefix(s, name) {
			if o, err := strconv.Atoi(s[2:]); err == nil {
				return o*12 + i, nil
			}
		}
	}
	for i, name := range noteNames {
		if len(name) == 1 && strings.HasPrefix(s, name) {
			if o, err := strconv.Atoi(s[1:]); err == nil {
				return o*12 + i, nil
			}
		}
	}
	return 0, invalidValue("invalid key name: %s", s)
}

var noteSpecs = []*PropSpec{
	itProp("fine_pitch", KInt, "Linear. 0 = -1200 cents, 240 = +1200 cents, default 120.", "fine_pitch"),
	itProp("group", KInt, "A number shared by notes in the same group or 0 if ungrouped.", "group"),
	customProp("key", KString, "Note name with octave e.g. C5 or A#3 (C0-B10); only sharps are used. Accepts a number 0-131 or a name when set.",
		func(m *Model) (interface{}, bool) {
			v, ok := m.item.Int("key")
			if !ok {
				return nil, false
			}
			return NoteName(int(v)), true
		},
		func(m *Model, v interface{}) error {
			s, _ := v.(string)
			var key int
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				key = n
			} else {
				k, err := ParseNoteName(s)
				if err != nil {
					return err
				}
				key = k
			}
			if key < 0 || key > 131 {
				return invalidValue("expected a value between 0-131")
			}
			return m.setFieldValue(0, "key", int64(key))
		}),
	itProp("length", KInt, "Returns 0 for notes punched in through step sequencer.", "length"),
	itProp("midi_channel", KInt, "Used for note colors (0-15); +128 for MIDI dragged into the piano roll.", "midi_channel"),
	itProp("mod_x", KInt, "Plugin configurable parameter. 0-255, default 128.", "mod_x"),
	itProp("mod_y", KInt, "Plugin configurable parameter. 0-255, default 128.", "mod_y"),
	itProp("pan", KInt, "0 = 100% left, 128 = 100% right, default 64.", "pan"),
	itProp("position", KInt, "Position of the note (PPQ dependant).", "position"),
	itProp("rack_channel", KInt, "IID of the containing channel.", "rack_channel"),
	itProp("release", KInt, "0-128, default 64.", "release"),
	flagProp("slide", "Whether note is a sliding note.", 0, "flags", NoteSlide, false),
	itProp("velocity", KInt, "0-128, default 100.", "velocity"),
}

var patternControllerSpecs = []*PropSpec{
	itProp("channel", KInt, "IID of the containing channel.", "channel"),
	itProp("position", KInt, "Position of the controller event.", "position"),
	itProp("value", KFloat, "Value of the controller event.", "value"),
}

var patternSpecs = []*PropSpec{
	evProp("color", KColor, "Colour of the pattern, if one was set. Defaults to #485156 in FL Studio.", IDPatternColor),
	customProp("iid", KInt, "Internal index of the pattern starting from 1. Changing it will not resolve collisions.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDPatternNew)
			if e == nil {
				return nil, false
			}
			return e.Value(), true
		},
		func(m *Model, v interface{}) error {
			evs := m.Events.Get(IDPatternNew)
			if len(evs) == 0 {
				return cannotSet(IDPatternNew)
			}
			for _, e := range evs {
				if err := e.SetValue(v); err != nil {
					return err
				}
			}
			return nil
		}),
	evProp("length", KInt, "Number of steps multiplied by the PPQ. Absent if the pattern is in Auto mode.", IDPatternLength),
	evPropDefault("looped", KBool, "Whether a pattern is in live loop mode.", false, IDPatternLooped),
	evProp("name", KString, "User given name of the pattern.", IDPatternName),
}

// Pattern represents a pattern which can contain notes, controllers and time
// markers.
type Pattern struct{ *Model }

// IID returns the internal pattern index (1-based).
func (p *Pattern) IID() int64 { v, _ := p.Int("iid"); return v }

// Notes returns the MIDI notes contained inside the pattern.
func (p *Pattern) Notes() []*Model {
	e := p.Events.FirstOf(IDPatternNotes)
	if e == nil {
		return nil
	}
	list, _ := e.List()
	out := make([]*Model, len(list))
	for i, it := range list {
		out[i] = newItemModel("Note", it, i, e, noteSpecs)
	}
	return out
}

// Controllers returns the parameter automations associated with the pattern.
func (p *Pattern) Controllers() []*Model {
	e := p.Events.FirstOf(IDPatternControllers)
	if e == nil {
		return nil
	}
	list, _ := e.List()
	out := make([]*Model, len(list))
	for i, it := range list {
		out[i] = newItemModel("Controller", it, i, e, patternControllerSpecs)
	}
	return out
}

// TimeMarkers returns the timemarkers inside the pattern.
func (p *Pattern) TimeMarkers() []*TimeMarker { return timeMarkersOf(p.Events) }

func (p *Pattern) String() string {
	name, _ := p.Str("name")
	return fmt.Sprintf("Pattern(iid=%d, name=%q, %d notes, %d controllers)",
		p.IID(), name, len(p.Notes()), len(p.Controllers()))
}

var patternsSpecs = []*PropSpec{
	evProp("play_cut_notes", KBool, "Whether truncated notes of patterns placed in the playlist should be played.", IDPatternsPlayTruncatedNotes),
}

// Patterns is the collection of patterns in a project.
type Patterns struct{ *Model }

// All returns every pattern in the project in order of appearance.
func (ps *Patterns) All() []*Pattern {
	cur := int64(0)
	groups := map[int64][]*IndexedEvent{}
	var order []int64
	for _, ie := range ps.Events.lst {
		if ie.E.id == IDPatternNew {
			cur, _ = ie.E.Int()
		}
		if InGroups(ie.E.id, GroupPattern, GroupTimeMarker) {
			if _, ok := groups[cur]; !ok {
				order = append(order, cur)
			}
			groups[cur] = append(groups[cur], ie)
		}
	}
	out := make([]*Pattern, 0, len(order))
	for _, k := range order {
		et := NewEventTree(ps.Events, groups[k])
		out = append(out, &Pattern{newModel("Pattern", et, patternSpecs)})
	}
	return out
}

// Len returns the number of patterns; ErrNoModelsFound if there are none.
func (ps *Patterns) Len() (int, error) {
	if !ps.Events.Contains(IDPatternNew) {
		return 0, ErrNoModelsFound
	}
	seen := map[int64]bool{}
	for _, e := range ps.Events.Get(IDPatternNew) {
		v, _ := e.Int()
		seen[v] = true
	}
	return len(seen), nil
}

// At returns the pattern at a zero-based index.
func (ps *Patterns) At(i int) (*Pattern, error) {
	all := ps.All()
	if i < 0 || i >= len(all) {
		return nil, modelNotFound(i)
	}
	return all[i], nil
}

// ByName returns the pattern with the given name.
func (ps *Patterns) ByName(name string) (*Pattern, error) {
	for _, p := range ps.All() {
		if n, ok := p.Str("name"); ok && n == name {
			return p, nil
		}
	}
	return nil, modelNotFound(name)
}

// ByIID returns the pattern with the given internal index.
func (ps *Patterns) ByIID(iid int64) (*Pattern, error) {
	for _, p := range ps.All() {
		if p.IID() == iid {
			return p, nil
		}
	}
	return nil, modelNotFound(iid)
}

// Current returns the currently selected pattern (nil if unknown).
func (ps *Patterns) Current() *Pattern {
	e := ps.Events.FirstOf(IDPatternsCurrentlySelected)
	if e == nil {
		return nil
	}
	idx, _ := e.Int()
	for _, p := range ps.All() {
		if p.IID() == idx {
			return p
		}
	}
	return nil
}

func (ps *Patterns) String() string {
	all := ps.All()
	iids := make([]int64, len(all))
	for i, p := range all {
		iids[i] = p.IID()
	}
	return fmt.Sprintf("%d Patterns %v", len(iids), iids)
}
