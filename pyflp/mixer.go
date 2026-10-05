package pyflp

import (
	"fmt"
	"sort"
)

// insertItems indexes the items of a MixerParamsEvent which belong to one
// insert. The containers are shared with the event, so editing them and
// committing the event changes the file.
type insertItems struct {
	slots    map[int]map[int64]*Container
	own      map[int64]*Container
	ownOrder []int64
	event    *Event
}

func newInsertItems(ev *Event) *insertItems {
	return &insertItems{
		slots: map[int]map[int64]*Container{},
		own:   map[int64]*Container{},
		event: ev,
	}
}

// mixerParamItems groups the items of the MixerParams event by insert index.
func mixerParamItems(ev *Event) map[int]*insertItems {
	out := map[int]*insertItems{}
	list, _ := ev.List()
	for _, item := range list {
		cd, _ := item.Int("channel_data")
		insertIdx := int((cd >> 6) & 0x7F)
		slotIdx := int(cd & 0x3F)
		id, _ := item.Int("id")
		ins, ok := out[insertIdx]
		if !ok {
			ins = newInsertItems(ev)
			out[insertIdx] = ins
		}
		if id == MixerSlotEnabled || id == MixerSlotMix {
			s, ok := ins.slots[slotIdx]
			if !ok {
				s = map[int64]*Container{}
				ins.slots[slotIdx] = s
			}
			s[id] = item
		} else {
			if _, exists := ins.own[id]; !exists {
				ins.ownOrder = append(ins.ownOrder, id)
			}
			ins.own[id] = item
		}
	}
	return out
}

func insertItemsOf(m *Model) *insertItems {
	if v, ok := m.kw["params"]; ok {
		if it, ok := v.(*insertItems); ok {
			return it
		}
	}
	return nil
}

// mixerParam is an Insert property backed by an item of the mixer params.
func mixerParam(name string, kind PropKind, doc string, id int64) *PropSpec {
	return customProp(name, kind, doc,
		func(m *Model) (interface{}, bool) {
			it := insertItemsOf(m)
			if it == nil {
				return nil, false
			}
			item, ok := it.own[id]
			if !ok {
				return nil, false
			}
			return item.Int("msg")
		},
		func(m *Model, v interface{}) error {
			it := insertItemsOf(m)
			if it == nil {
				return cannotSet()
			}
			item, ok := it.own[id]
			if !ok {
				return fmt.Errorf("%w: mixer parameter %d", ErrPropertyCannotBeSet, id)
			}
			return setMsg(item, it.event, v)
		})
}

func setMsg(item *Container, ev *Event, v interface{}) error {
	old, _ := item.Get("msg")
	item.Set("msg", v)
	if err := ev.Commit(); err != nil {
		item.Set("msg", old)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Insert EQ
// ---------------------------------------------------------------------------

func eqBandProp(name, doc, key string) *PropSpec {
	return customProp(name, KInt, doc,
		func(m *Model) (interface{}, bool) {
			c, ok := m.kw[key].(*Container)
			if !ok {
				return nil, false
			}
			return c.Int("msg")
		},
		func(m *Model, v interface{}) error {
			c, ok := m.kw[key].(*Container)
			ev, _ := m.kw["event"].(*Event)
			if !ok || ev == nil {
				return cannotSet()
			}
			return setMsg(c, ev, v)
		})
}

var eqBandSpecs = []*PropSpec{
	eqBandProp("gain", "Min -1800, Max 1800, Default 0.", "gain"),
	eqBandProp("freq", "Nonlinear. Min 0 (10 Hz), Max 65536 (16 kHz).", "freq"),
	eqBandProp("reso", "Min 0, Max 65536, Default 17500.", "reso"),
}

// InsertEQ is the post-effect 3-band EQ of an insert.
type InsertEQ struct {
	params *insertItems
}

func (q *InsertEQ) band(freq, gain, reso int64) *Model {
	m := newModel("InsertEQBand", nil, eqBandSpecs)
	if q.params == nil {
		return m
	}
	m.kw["event"] = q.params.event
	for id, item := range q.params.own {
		switch id {
		case freq:
			m.kw["freq"] = item
		case gain:
			m.kw["gain"] = item
		case reso:
			m.kw["reso"] = item
		}
	}
	return m
}

// Low shelf band. Default frequency - 5777 (90 Hz).
func (q *InsertEQ) Low() *Model { return q.band(MixerLowFreq, MixerLowGain, MixerLowQ) }

// Mid is the middle band. Default frequency - 33145 (1500 Hz).
func (q *InsertEQ) Mid() *Model { return q.band(MixerMidFreq, MixerMidGain, MixerMidQ) }

// High shelf band. Default frequency - 55825 (8000 Hz).
func (q *InsertEQ) High() *Model { return q.band(MixerHighFreq, MixerHighGain, MixerHighQ) }

// ---------------------------------------------------------------------------
// Slot
// ---------------------------------------------------------------------------

func slotParam(name string, kind PropKind, doc string, id int64) *PropSpec {
	get := func(m *Model) (interface{}, bool) {
		p, _ := m.kw["slot"].(map[int64]*Container)
		item, ok := p[id]
		if !ok {
			return nil, false
		}
		n, ok := item.Int("msg")
		if !ok {
			return nil, false
		}
		if kind == KBool {
			return n != 0, true
		}
		return n, true
	}
	return customProp(name, kind, doc, get, func(m *Model, v interface{}) error {
		p, _ := m.kw["slot"].(map[int64]*Container)
		ev, _ := m.kw["event"].(*Event)
		item, ok := p[id]
		if !ok || ev == nil {
			return fmt.Errorf("%w: mixer parameter %d", ErrPropertyCannotBeSet, id)
		}
		if b, isBool := v.(bool); isBool {
			if b {
				v = int64(1)
			} else {
				v = int64(0)
			}
		}
		return setMsg(item, ev, v)
	})
}

var slotSpecs = []*PropSpec{
	evProp("color", KColor, "Colour of the slot.", IDPluginColor),
	evProp("iid", KInt, "A 0-based internal index.", IDSlotIndex),
	evProp("internal_name", KString, "'Fruity Wrapper' for VST/AU plugins or factory name for native plugins.", IDPluginInternalName),
	slotParam("enabled", KBool, "Whether the slot is enabled.", MixerSlotEnabled),
	evProp("icon", KInt, "Internal ID of the icon.", IDPluginIcon),
	evProp("index", KInt, "A 0-based internal index.", IDSlotIndex),
	slotParam("mix", KInt, "Dry/Wet mix. -6400 (100% left) to 6400 (100% right), default 0.", MixerSlotMix),
	evProp("name", KString, "Name of the slot.", IDPluginName),
}

var effectKinds = []string{"VSTPlugin", "FruityBalance", "FruityBloodOverdrive", "FruityCenter",
	"FruityFastDist", "FruityNotebook2", "FruitySend", "FruitySoftClipper", "FruityStereoEnhancer",
	"Soundgoodizer"}

// Slot is an effect slot of an insert.
type Slot struct{ *Model }

// Plugin returns the effect loaded into the slot (nil for empty slots).
func (s *Slot) Plugin() *Plugin { return pluginOf(s.Events, effectKinds...) }

// SetPlugin replaces the effect in the slot.
func (s *Slot) SetPlugin(p *Plugin) error { return setPlugin(s.Events, p) }

func (s *Slot) String() string {
	name, _ := s.Str("name")
	idx, _ := s.Int("index")
	pl := "<none>"
	if p := s.Plugin(); p != nil {
		pl = p.Kind
	}
	return fmt.Sprintf("Slot (name=%q, iid=%d, plugin=%s)", name, idx, pl)
}

// ---------------------------------------------------------------------------
// Insert
// ---------------------------------------------------------------------------

func insertFlag(name, doc string, mask int64, inverted bool) *PropSpec {
	return flagProp(name, doc, IDInsertFlags, "flags", mask, inverted)
}

var insertSpecs = []*PropSpec{
	insertFlag("bypassed", "Whether all slots are bypassed.", InsertEnableEffects, true),
	insertFlag("channels_swapped", "Whether the left and right channels are swapped.", InsertSwapLeftRight, false),
	evProp("color", KColor, "Defaults to #636C71 (granite gray). Values below 20 for any component are ignored by FL. New in FL Studio v4.0.", IDInsertColor),
	readOnly(withEnum(customProp("dock", KEnum, "The position (left, middle or right) where the insert is docked in the mixer.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDInsertFlags)
			if e == nil {
				return nil, false
			}
			c, _ := e.Container()
			if c == nil {
				return nil, false
			}
			f, ok := c.Int("flags")
			if !ok {
				return nil, false
			}
			switch {
			case f&InsertDockMiddle != 0:
				return int64(DockMiddle), true
			case f&InsertDockRight != 0:
				return int64(DockRight), true
			}
			return int64(DockLeft), true
		}, nil), InsertDockEnum)),
	insertFlag("enabled", "Whether an insert in the mixer is enabled or disabled.", InsertEnabled, false),
	evProp("icon", KInt, "Internal ID of the icon shown beside the name.", IDInsertIcon),
	evProp("input", KInt, "Input source of the insert.", IDInsertInput),
	insertFlag("is_solo", "Whether the insert is solo'd.", InsertSolo, false),
	insertFlag("locked", "Whether an insert in the mixer is in locked state.", InsertLocked, false),
	evProp("name", KString, "Name of the insert. New in FL Studio v3.5.4.", IDInsertName),
	evProp("output", KInt, "Output target of the insert.", IDInsertOutput),
	mixerParam("pan", KInt, "Linear. -6400 (100% left) to 6400 (100% right), default 0.", MixerPan),
	insertFlag("polarity_reversed", "Whether phase / polarity is reversed / inverted.", InsertPolarityReversed, false),
	insertFlag("separator_shown", "Whether a separator is shown before the insert.", InsertSeparatorShown, false),
	mixerParam("stereo_separation", KInt, "Linear. 64 = 100% merged, -64 = 100% separated, default 0.", MixerStereoSeparation),
	mixerParam("volume", KInt, "Post volume fader. Logarithmic. 0 to 16000, default 12800 (0.0dB).", MixerVolume),
}

// Insert is a mixer track to which channels from the rack are routed.
type Insert struct{ *Model }

// IID returns -1 for the "current" insert, 0 for master and up to
// Mixer.MaxInserts.
func (i *Insert) IID() int { v, _ := i.kw["iid"].(int); return v }

func (i *Insert) String() string {
	name, _ := i.Str("name")
	return fmt.Sprintf("Insert(name=%q, iid=%d)", name, i.IID())
}

// Slots returns the effect slots (empty and used) of the insert.
func (i *Insert) Slots() []*Slot {
	var out []*Slot
	params := insertItemsOf(i.Model)
	divIDs := []EventID{IDSlotIndex}
	for _, id := range AllIDs() {
		if InGroups(id, GroupSlot, GroupPlugin) && id != IDSlotIndex {
			divIDs = append(divIDs, id)
		}
	}
	for idx, ed := range i.Events.Divide(IDSlotIndex, divIDs...) {
		m := newModel("Slot", ed, slotSpecs)
		if params != nil {
			m.kw["slot"] = params.slots[idx]
			m.kw["event"] = params.event
		}
		out = append(out, &Slot{m})
	}
	return out
}

// Len returns the number of slots.
func (i *Insert) Len() int {
	if i.Events.Contains(IDSlotIndex) {
		return i.Events.Count(IDSlotIndex)
	}
	return len(i.Slots())
}

// SlotAt returns the slot at a zero-based index.
func (i *Insert) SlotAt(idx int) (*Slot, error) {
	all := i.Slots()
	if idx < 0 || idx >= len(all) {
		return nil, modelNotFound(idx)
	}
	return all[idx], nil
}

// SlotByName returns the slot with the name.
func (i *Insert) SlotByName(name string) (*Slot, error) {
	for _, s := range i.Slots() {
		if n, ok := s.Str("name"); ok && n == name {
			return s, nil
		}
	}
	return nil, modelNotFound(name)
}

// EQ returns the 3-band post EQ.
func (i *Insert) EQ() *InsertEQ { return &InsertEQ{params: insertItemsOf(i.Model)} }

// Routes returns the send volumes to routed inserts (New in FL Studio v4.0).
func (i *Insert) Routes() []int64 {
	it := insertItemsOf(i.Model)
	re := i.Events.FirstOf(IDInsertRouting)
	if it == nil || re == nil {
		return nil
	}
	flags, _ := re.Value().([]bool)
	var out []int64
	n := 0
	for _, id := range it.ownOrder {
		if id < MixerRouteVolStart || id >= MixerVolume {
			continue
		}
		if n >= len(flags) {
			continue
		}
		cond := flags[n]
		n++
		if cond {
			v, _ := it.own[id].Int("msg")
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Mixer
// ---------------------------------------------------------------------------

type verLimit struct {
	k [3]int
	v int
}

var maxInserts = []verLimit{
	{[3]int{1, 6, 5}, 5}, {[3]int{2, 0, 1}, 8}, {[3]int{3, 0, 0}, 18}, {[3]int{3, 3, 0}, 20},
	{[3]int{4, 0, 0}, 64}, {[3]int{9, 0, 0}, 105}, {[3]int{12, 9, 0}, 127},
}

var maxSlots = []verLimit{{[3]int{1, 6, 5}, 4}, {[3]int{3, 0, 0}, 8}}

// versionLE mirrors PyFLP's `dataclasses.astuple(version) <= k`: the version
// is a 4-tuple (with the build) and k a 3-tuple, so when the first three
// numbers are equal the version is the greater one.
func versionLE(v FLVersion, k [3]int) bool {
	a := [3]int{v.Major, v.Minor, v.Patch}
	for i := 0; i < 3; i++ {
		if a[i] != k[i] {
			return a[i] < k[i]
		}
	}
	return false
}

var mixerSpecs = []*PropSpec{
	customProp("apdc", KBool, "Whether automatic plugin delay compensation is enabled for the inserts.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDMixerAPDC)
			if e == nil {
				return nil, false
			}
			n, _ := e.Int()
			return n != 0, true
		},
		func(m *Model, v interface{}) error {
			b, _ := v.(bool)
			n := int64(0)
			if b {
				n = 1
			}
			return m.setEventValue(n, IDMixerAPDC)
		}),
}

// Mixer is the mixer which contains Insert instances.
type Mixer struct{ *Model }

func (m *Mixer) version() FLVersion {
	v, _ := m.kw["version"].(FLVersion)
	return v
}

// MaxInserts is the estimated max number of inserts including sends, master
// and current.
func (m *Mixer) MaxInserts() int {
	v := m.version()
	for _, l := range maxInserts {
		if versionLE(v, l.k) {
			return l.v
		}
	}
	return 127
}

// MaxSlots is the estimated max number of effect slots per insert.
func (m *Mixer) MaxSlots() int {
	v := m.version()
	for _, l := range maxSlots {
		if versionLE(v, l.k) {
			return l.v
		}
	}
	return 10
}

// All returns the inserts in the project.
func (m *Mixer) All() []*Insert {
	sel := func(e *Event) Select {
		if e.id == IDInsertOutput {
			return Cut
		}
		if InGroups(e.id, GroupInsert, GroupPlugin, GroupSlot) {
			return Include
		}
		return Skip
	}
	var params map[int]*insertItems
	if pe := m.Events.FirstOf(IDMixerParams); pe != nil {
		params = mixerParamItems(pe)
	}
	var out []*Insert
	for i, ed := range m.Events.Subtrees(sel, m.MaxInserts()) {
		im := newModel("Insert", ed, insertSpecs)
		im.kw["iid"] = i - 1
		im.kw["max_slots"] = m.MaxSlots()
		if it, ok := params[i]; ok {
			im.kw["params"] = it
		}
		out = append(out, &Insert{im})
	}
	return out
}

// Len returns the number of inserts; ErrNoModelsFound if there are none.
func (m *Mixer) Len() (int, error) {
	if !m.Events.Contains(IDInsertFlags) {
		return 0, ErrNoModelsFound
	}
	return m.Events.Count(IDInsertFlags), nil
}

// At returns an insert by the index FL Studio shows: 0 is master and -1 the
// "current" insert.
func (m *Mixer) At(i int) (*Insert, error) {
	all := m.All()
	if i+1 < 0 || i+1 >= len(all) {
		return nil, modelNotFound(i)
	}
	return all[i+1], nil
}

// ByName returns the insert with the name.
func (m *Mixer) ByName(name string) (*Insert, error) {
	for _, in := range m.All() {
		if n, ok := in.Str("name"); ok && n == name {
			return in, nil
		}
	}
	return nil, modelNotFound(name)
}

func (m *Mixer) String() string {
	n, _ := m.Len()
	return fmt.Sprintf("Mixer: %d inserts", n)
}

// sortedKeys is a small helper for deterministic output.
func sortedKeys(m map[int64]*Container) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
