package pyflp

import (
	"fmt"
	"strconv"
	"strings"
)

// Path addressing
//
// Properties of any model can be addressed with a dotted path, optionally
// with [key] selectors where a collection is involved:
//
//	title
//	tempo
//	channels[0].volume                  (key: internal index or display name)
//	channels[Kick].delay.time
//	channels[3].plugin.width
//	channels[3].tracking[volume].pan
//	channels[1].envelopes[Volume].attack
//	channels[2].points[0].value
//	patterns[1].name                    (key: iid or name)
//	patterns[1].notes[0].velocity
//	mixer[0].volume                     (0 = master, -1 = current, or a name)
//	mixer[1].eq.low.gain
//	mixer[1].slots[0].plugin.mix
//	arrangements[0].tracks[1].name
//	arrangements[0].tracks[1].items[0].length
//	arrangements[0].timemarkers[0].name
//	arrangements.time_signature.num
//	arrangements.current.name
//
// A leading "project." is optional.

type pathSeg struct {
	name string
	key  string
	idx  bool
}

func parsePath(path string) ([]pathSeg, error) {
	var segs []pathSeg
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, invalidValue("empty path segment in %q", path)
		}
		s := pathSeg{name: part}
		if i := strings.Index(part, "["); i >= 0 {
			if !strings.HasSuffix(part, "]") {
				return nil, invalidValue("unterminated [ in %q", part)
			}
			s.name = part[:i]
			s.key = part[i+1 : len(part)-1]
			s.idx = true
		}
		segs = append(segs, s)
	}
	return segs, nil
}

// modelOf returns the Model backing a navigation object.
func modelOf(cur interface{}) *Model {
	switch x := cur.(type) {
	case *Model:
		return x
	case *Project:
		return x.Model
	case *ChannelRack:
		return x.Model
	case *Channel:
		return x.Model
	case *Patterns:
		return x.Model
	case *Pattern:
		return x.Model
	case *Mixer:
		return x.Model
	case *Insert:
		return x.Model
	case *Slot:
		return x.Model
	case *Plugin:
		return x.Model
	case *Arrangements:
		return x.Model
	case *Arrangement:
		return x.Model
	case *Track:
		return x.Model
	case *PLItem:
		return x.Model
	case *TimeMarker:
		return x.Model
	case *NamedModel:
		return x.Model
	}
	return nil
}

func keyInt(key string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(key), 0, 64)
	return n, err == nil
}

func pick(kind string, n int, key string) (int, error) {
	i, ok := keyInt(key)
	if !ok || i < 0 || int(i) >= n {
		return 0, modelNotFound(fmt.Sprintf("%s[%s]", kind, key))
	}
	return int(i), nil
}

func namedPick(list []NamedModel, key string) (*Model, error) {
	if i, ok := keyInt(key); ok && i >= 0 && int(i) < len(list) {
		return list[i].Model, nil
	}
	for _, nm := range list {
		if strings.EqualFold(nm.Name, key) {
			return nm.Model, nil
		}
	}
	return nil, modelNotFound(key)
}

func needKey(s pathSeg) error {
	if !s.idx {
		return invalidValue("%q needs a [key]", s.name)
	}
	return nil
}

func child(cur interface{}, s pathSeg) (interface{}, error) {
	switch x := cur.(type) {
	case *Project:
		switch s.name {
		case "channels":
			if s.idx {
				return child(x.Channels(), pathSeg{name: "channels", key: s.key, idx: true})
			}
			return x.Channels(), nil
		case "patterns":
			if s.idx {
				return child(x.Patterns(), s)
			}
			return x.Patterns(), nil
		case "mixer":
			if s.idx {
				return child(x.Mixer(), s)
			}
			return x.Mixer(), nil
		case "arrangements":
			if s.idx {
				return child(x.Arrangements(), s)
			}
			return x.Arrangements(), nil
		}
	case *ChannelRack:
		switch s.name {
		case "channels":
			if err := needKey(s); err != nil {
				return nil, err
			}
			if iid, ok := keyInt(s.key); ok {
				return x.ByIID(iid)
			}
			return x.ByName(s.key)
		case "groups":
			if err := needKey(s); err != nil {
				return nil, err
			}
			g := x.Groups()
			i, err := pick("groups", len(g), s.key)
			if err != nil {
				return nil, err
			}
			return g[i], nil
		}
	case *Channel:
		switch s.name {
		case "keyboard":
			return x.Keyboard(), nil
		case "arp":
			return x.Arp(), nil
		case "delay":
			return x.Delay(), nil
		case "level_adjusts":
			return x.LevelAdjusts(), nil
		case "polyphony":
			return x.Polyphony(), nil
		case "time":
			return x.Time(), nil
		case "content":
			return x.Content(), nil
		case "filter":
			return x.Filter(), nil
		case "fx":
			return x.FX(), nil
		case "reverb":
			return x.Reverb(), nil
		case "playback":
			return x.Playback(), nil
		case "stretching":
			return x.Stretching(), nil
		case "lfo":
			return x.LFO(), nil
		case "group":
			if g := x.Group(); g != nil {
				return g, nil
			}
			return nil, modelNotFound("group")
		case "plugin":
			if p := x.Plugin(); p != nil {
				return p, nil
			}
			return nil, modelNotFound("plugin")
		case "tracking", "envelopes", "lfos":
			var list []NamedModel
			switch s.name {
			case "tracking":
				list = x.Tracking()
			case "envelopes":
				list = x.Envelopes()
			default:
				list = x.LFOs()
			}
			if err := needKey(s); err != nil {
				return nil, err
			}
			return namedPick(list, s.key)
		case "points":
			if err := needKey(s); err != nil {
				return nil, err
			}
			pts := x.Points()
			i, err := pick("points", len(pts), s.key)
			if err != nil {
				return nil, err
			}
			return pts[i], nil
		case "children":
			if err := needKey(s); err != nil {
				return nil, err
			}
			kids, err := x.Children()
			if err != nil {
				return nil, err
			}
			i, err := pick("children", len(kids), s.key)
			if err != nil {
				return nil, err
			}
			return kids[i], nil
		}
	case *Patterns:
		switch s.name {
		case "patterns":
			if err := needKey(s); err != nil {
				return nil, err
			}
			if iid, ok := keyInt(s.key); ok {
				return x.ByIID(iid)
			}
			return x.ByName(s.key)
		case "current":
			if p := x.Current(); p != nil {
				return p, nil
			}
			return nil, modelNotFound("current pattern")
		}
	case *Pattern:
		var list []*Model
		switch s.name {
		case "notes":
			list = x.Notes()
		case "controllers":
			list = x.Controllers()
		case "timemarkers":
			tm := x.TimeMarkers()
			if err := needKey(s); err != nil {
				return nil, err
			}
			i, err := pick("timemarkers", len(tm), s.key)
			if err != nil {
				return nil, err
			}
			return tm[i], nil
		default:
			return nil, nil
		}
		if err := needKey(s); err != nil {
			return nil, err
		}
		i, err := pick(s.name, len(list), s.key)
		if err != nil {
			return nil, err
		}
		return list[i], nil
	case *Mixer:
		switch s.name {
		case "mixer", "inserts":
			if err := needKey(s); err != nil {
				return nil, err
			}
			if i, ok := keyInt(s.key); ok {
				return x.At(int(i))
			}
			return x.ByName(s.key)
		}
	case *Insert:
		switch s.name {
		case "eq":
			return x.EQ(), nil
		case "slots":
			if err := needKey(s); err != nil {
				return nil, err
			}
			if i, ok := keyInt(s.key); ok {
				return x.SlotAt(int(i))
			}
			return x.SlotByName(s.key)
		}
	case *InsertEQ:
		switch s.name {
		case "low":
			return x.Low(), nil
		case "mid":
			return x.Mid(), nil
		case "high":
			return x.High(), nil
		}
	case *Slot:
		if s.name == "plugin" {
			if p := x.Plugin(); p != nil {
				return p, nil
			}
			return nil, modelNotFound("plugin")
		}
	case *Plugin:
		if !x.IsVST() {
			return nil, nil
		}
		switch s.name {
		case "automation":
			return x.Automation(), nil
		case "compatibility":
			return x.Compatibility(), nil
		case "midi":
			return x.MIDI(), nil
		case "processing":
			return x.Processing(), nil
		case "ui":
			return x.UI(), nil
		}
	case *Arrangements:
		switch s.name {
		case "arrangements":
			if err := needKey(s); err != nil {
				return nil, err
			}
			if i, ok := keyInt(s.key); ok {
				return x.At(int(i))
			}
			return x.ByName(s.key)
		case "current":
			a, err := x.Current()
			if err != nil {
				return nil, err
			}
			if a == nil {
				return nil, modelNotFound("current arrangement")
			}
			return a, nil
		case "time_signature":
			return x.TimeSignature(), nil
		}
	case *Arrangement:
		switch s.name {
		case "timemarkers":
			tm := x.TimeMarkers()
			if err := needKey(s); err != nil {
				return nil, err
			}
			i, err := pick("timemarkers", len(tm), s.key)
			if err != nil {
				return nil, err
			}
			return tm[i], nil
		case "tracks":
			if err := needKey(s); err != nil {
				return nil, err
			}
			tr := x.Tracks()
			if i, ok := keyInt(s.key); ok {
				if i < 0 || int(i) >= len(tr) {
					return nil, modelNotFound(fmt.Sprintf("tracks[%s]", s.key))
				}
				return tr[i], nil
			}
			for _, t := range tr {
				if n, ok := t.Str("name"); ok && n == s.key {
					return t, nil
				}
			}
			return nil, modelNotFound(s.key)
		}
	case *Track:
		if s.name == "items" {
			if err := needKey(s); err != nil {
				return nil, err
			}
			i, err := pick("items", x.Len(), s.key)
			if err != nil {
				return nil, err
			}
			return x.items[i], nil
		}
	}
	return nil, nil
}

// Resolve finds the model and property addressed by path (see the package
// comment of path.go for the syntax).
func (p *Project) Resolve(path string) (*Model, string, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, "", err
	}
	if segs[0].name == "project" && !segs[0].idx && len(segs) > 1 {
		segs = segs[1:]
	}
	var cur interface{} = p
	for i, s := range segs {
		last := i == len(segs)-1
		if last && !s.idx {
			if m := modelOf(cur); m != nil && m.Spec(s.name) != nil {
				return m, s.name, nil
			}
		}
		next, err := child(cur, s)
		if err != nil {
			return nil, "", err
		}
		if next == nil {
			return nil, "", fmt.Errorf("%w: cannot resolve %q in %q", ErrModelNotFound, s.name, path)
		}
		cur = next
	}
	return nil, "", invalidValue("%q addresses an object, not a property", path)
}

// Get reads the property addressed by path. The returned value is formatted
// for display (see FormatValue).
func (p *Project) Get(path string, full bool) (interface{}, error) {
	m, name, err := p.Resolve(path)
	if err != nil {
		return nil, err
	}
	v, ok := m.Get(name)
	if !ok {
		return nil, nil
	}
	return FormatValue(m.Spec(name), v, full), nil
}

// SetPath writes the property addressed by path; value may be a string.
func (p *Project) SetPath(path string, value interface{}) error {
	m, name, err := p.Resolve(path)
	if err != nil {
		return err
	}
	return m.Set(name, value)
}

// ResolveModel finds the model (object) addressed by path, e.g.
// "channels[Kick].delay". Use it to inspect all properties of an object.
func (p *Project) ResolveModel(path string) (*Model, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if segs[0].name == "project" && !segs[0].idx {
		segs = segs[1:]
	}
	var cur interface{} = p
	for _, s := range segs {
		next, err := child(cur, s)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, fmt.Errorf("%w: cannot resolve %q in %q", ErrModelNotFound, s.name, path)
		}
		cur = next
	}
	m := modelOf(cur)
	if m == nil {
		return nil, invalidValue("%q does not address a model with properties", path)
	}
	return m, nil
}
