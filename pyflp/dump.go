package pyflp

import "fmt"

func nonEmpty(m *Model, full bool) (*OrderedMap, bool) {
	d := m.Dump(full)
	return d, len(d.Keys) > 0
}

func dumpPlugin(p *Plugin, full bool) *OrderedMap {
	o := NewOrderedMap()
	o.Set("type", p.Kind)
	if p.InternalName != "" {
		o.Set("internal_name", p.InternalName)
	}
	for _, k := range p.Dump(full).Keys {
		o.Set(k, p.Dump(full).Vals[k])
	}
	if p.IsVST() {
		subs := []struct {
			n string
			m *Model
		}{
			{"automation", p.Automation()}, {"compatibility", p.Compatibility()},
			{"midi", p.MIDI()}, {"processing", p.Processing()}, {"ui", p.UI()},
		}
		for _, s := range subs {
			if d, ok := nonEmpty(s.m, full); ok {
				o.Set(s.n, d)
			}
		}
	}
	return o
}

func dumpNamed(list []NamedModel, full bool) *OrderedMap {
	o := NewOrderedMap()
	for _, nm := range list {
		o.Set(nm.Name, nm.Dump(full))
	}
	return o
}

func dumpChannel(c *Channel, full bool) *OrderedMap {
	o := NewOrderedMap()
	o.Set("class", c.Class.String())
	d := c.Dump(full)
	for _, k := range d.Keys {
		o.Set(k, d.Vals[k])
	}
	if g := c.Group(); g != nil {
		if n, ok := g.Str("name"); ok {
			o.Set("group", n)
		}
	}
	add := func(name string, m *Model) {
		if d, ok := nonEmpty(m, full); ok {
			o.Set(name, d)
		}
	}
	switch c.Class {
	case ClassSampler, ClassInstrument:
		add("keyboard", c.Keyboard())
		add("arp", c.Arp())
		add("delay", c.Delay())
		add("level_adjusts", c.LevelAdjusts())
		add("polyphony", c.Polyphony())
		add("time", c.Time())
		if t := c.Tracking(); len(t) > 0 {
			o.Set("tracking", dumpNamed(t, full))
		}
	}
	switch c.Class {
	case ClassSampler:
		add("content", c.Content())
		add("filter", c.Filter())
		add("fx", c.FX())
		add("reverb", c.Reverb())
		add("playback", c.Playback())
		add("stretching", c.Stretching())
		if e := c.Envelopes(); len(e) > 0 {
			o.Set("envelopes", dumpNamed(e, full))
		}
		if l := c.LFOs(); len(l) > 0 {
			o.Set("lfos", dumpNamed(l, full))
		}
	case ClassInstrument:
		if p := c.Plugin(); p != nil {
			o.Set("plugin", dumpPlugin(p, full))
		}
	case ClassAutomation:
		add("lfo", c.LFO())
		pts := c.Points()
		if len(pts) > 0 {
			l := make([]interface{}, len(pts))
			for i, p := range pts {
				l[i] = p.Dump(full)
			}
			o.Set("points", l)
		}
	case ClassLayer:
		kids, _ := c.Children()
		if len(kids) > 0 {
			l := make([]interface{}, len(kids))
			for i, k := range kids {
				l[i] = k.IID()
			}
			o.Set("children", l)
		}
	}
	return o
}

func dumpTimeMarkers(list []*TimeMarker, full bool) []interface{} {
	out := make([]interface{}, len(list))
	for i, t := range list {
		d := t.Dump(full)
		d.Set("description", t.String())
		out[i] = d
	}
	return out
}

func dumpPattern(p *Pattern, full bool) *OrderedMap {
	o := p.Dump(full)
	if notes := p.Notes(); len(notes) > 0 {
		l := make([]interface{}, len(notes))
		for i, n := range notes {
			l[i] = n.Dump(full)
		}
		o.Set("notes", l)
	}
	if cs := p.Controllers(); len(cs) > 0 {
		l := make([]interface{}, len(cs))
		for i, n := range cs {
			l[i] = n.Dump(full)
		}
		o.Set("controllers", l)
	}
	if tm := p.TimeMarkers(); len(tm) > 0 {
		o.Set("timemarkers", dumpTimeMarkers(tm, full))
	}
	return o
}

func dumpInsert(in *Insert, full bool) *OrderedMap {
	o := NewOrderedMap()
	o.Set("iid", in.IID())
	d := in.Dump(full)
	for _, k := range d.Keys {
		o.Set(k, d.Vals[k])
	}
	if r := in.Routes(); len(r) > 0 {
		o.Set("routes", r)
	}
	if in.kw["params"] != nil {
		eq := NewOrderedMap()
		q := in.EQ()
		for _, b := range []struct {
			n string
			m *Model
		}{{"low", q.Low()}, {"mid", q.Mid()}, {"high", q.High()}} {
			if d, ok := nonEmpty(b.m, full); ok {
				eq.Set(b.n, d)
			}
		}
		if len(eq.Keys) > 0 {
			o.Set("eq", eq)
		}
	}
	var slots []interface{}
	for _, s := range in.Slots() {
		sd := s.Dump(full)
		if p := s.Plugin(); p != nil {
			sd.Set("plugin", dumpPlugin(p, full))
		}
		slots = append(slots, sd)
	}
	if len(slots) > 0 {
		o.Set("slots", slots)
	}
	return o
}

func dumpArrangement(a *Arrangement, full bool) *OrderedMap {
	o := a.Dump(full)
	if tm := a.TimeMarkers(); len(tm) > 0 {
		o.Set("timemarkers", dumpTimeMarkers(tm, full))
	}
	var tracks []interface{}
	for _, t := range a.Tracks() {
		td := t.Dump(full)
		var items []interface{}
		for _, it := range t.Items() {
			id := it.Dump(full)
			if it.Pattern != nil {
				n, _ := it.Pattern.Str("name")
				id.Set("pattern", n)
				id.Set("pattern_iid", it.Pattern.IID())
			}
			if it.Channel != nil {
				id.Set("channel", it.Channel.DisplayName())
				id.Set("channel_iid", it.Channel.IID())
			}
			items = append(items, id)
		}
		if len(items) > 0 {
			td.Set("items", items)
		}
		tracks = append(tracks, td)
	}
	if len(tracks) > 0 {
		o.Set("tracks", tracks)
	}
	return o
}

// Dump returns the whole project as an ordered tree, ready to be marshalled
// to JSON. Byte blobs (plugin states etc.) are summarised unless full is true.
func (p *Project) Dump(full bool) *OrderedMap {
	root := NewOrderedMap()
	root.Set("project", p.Model.Dump(full))

	rack := p.Channels()
	rackO := rack.Dump(full)
	if chs, err := rack.All(); err == nil || len(chs) > 0 {
		var l []interface{}
		for _, c := range chs {
			l = append(l, dumpChannel(c, full))
		}
		rackO.Set("channels", l)
	}
	var groups []interface{}
	for _, g := range rack.Groups() {
		groups = append(groups, g.Dump(full))
	}
	if len(groups) > 0 {
		rackO.Set("groups", groups)
	}
	root.Set("channel_rack", rackO)

	pats := p.Patterns()
	patO := pats.Dump(full)
	var pl []interface{}
	if _, err := pats.Len(); err == nil {
		for _, pt := range pats.All() {
			pl = append(pl, dumpPattern(pt, full))
		}
	}
	patO.Set("patterns", pl)
	if cur := pats.Current(); cur != nil {
		patO.Set("current", cur.IID())
	}
	root.Set("patterns", patO)

	mx := p.Mixer()
	mxO := mx.Dump(full)
	if _, err := mx.Len(); err == nil {
		var il []interface{}
		for _, in := range mx.All() {
			il = append(il, dumpInsert(in, full))
		}
		mxO.Set("inserts", il)
	}
	root.Set("mixer", mxO)

	arrs := p.Arrangements()
	arrO := arrs.Dump(full)
	arrO.Set("max_tracks", arrs.MaxTracks())
	if lp, ok := arrs.LoopPos(); ok {
		arrO.Set("loop_pos", []int64{lp[0], lp[1]})
	}
	if ts := arrs.TimeSignature().Dump(full); len(ts.Keys) > 0 {
		arrO.Set("time_signature", ts)
	}
	if list, err := arrs.All(); err == nil {
		var al []interface{}
		for _, a := range list {
			al = append(al, dumpArrangement(a, full))
		}
		arrO.Set("arrangements", al)
		if cur, err := arrs.Current(); err == nil && cur != nil {
			arrO.Set("current", cur.IID())
		}
	}
	root.Set("arrangements", arrO)

	if rc := p.RemoteControllers(); len(rc) > 0 {
		var l []interface{}
		for _, r := range rc {
			l = append(l, r.Dump(full))
		}
		root.Set("remote_controllers", l)
	}
	return root
}

// Summary returns a few human readable facts about the project.
func (p *Project) Summary() []string {
	var out []string
	add := func(k string, v interface{}) { out = append(out, fmt.Sprintf("%-14s %v", k+":", v)) }
	add("File", p.String())
	if t, ok := p.Str("title"); ok {
		add("Title", t)
	}
	if t, ok := p.Str("artists"); ok {
		add("Artists", t)
	}
	if t, ok := p.Float("tempo"); ok {
		add("Tempo", t)
	}
	add("PPQ", p.PPQ())
	if n, err := p.Channels().Len(); err == nil {
		add("Channels", n)
	}
	if n, err := p.Patterns().Len(); err == nil {
		add("Patterns", n)
	}
	if n, err := p.Mixer().Len(); err == nil {
		add("Inserts", n)
	}
	if n, err := p.Arrangements().Len(); err == nil {
		add("Arrangements", n)
	}
	add("Events", len(p.AllEvents()))
	return out
}

// EventValueJSON converts the decoded value of an event into something that
// marshals nicely to JSON (structs become ordered maps, blobs hex strings).
func EventValueJSON(v interface{}, full bool) interface{} {
	switch x := v.(type) {
	case *Container:
		o := NewOrderedMap()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			o.Set(k, EventValueJSON(val, full))
		}
		return o
	case []*Container:
		l := make([]interface{}, len(x))
		for i, c := range x {
			l[i] = EventValueJSON(c, full)
		}
		return l
	case []interface{}:
		l := make([]interface{}, len(x))
		for i, c := range x {
			l[i] = EventValueJSON(c, full)
		}
		return l
	}
	return FormatValue(nil, v, full)
}
