package pyflp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PropKind is the value type of a property.
type PropKind int

const (
	KInt      PropKind = iota // int64
	KFloat                    // float64
	KBool                     // bool
	KString                   // string
	KColor                    // RGBA
	KEnum                     // int64 (see PropSpec.Enum for the names)
	KPair                     // [2]int64
	KMusical                  // MusicalTime
	KBytes                    // []byte
	KStrMap                   // map[int64]string
	KDateTime                 // time.Time
	KDuration                 // time.Duration
	KVersion                  // FLVersion
)

func (k PropKind) String() string {
	return [...]string{"int", "float", "bool", "string", "color", "enum", "pair",
		"musical-time", "bytes", "page-map", "datetime", "duration", "version"}[k]
}

// EnumDef names the values of an integer enumeration.
type EnumDef struct {
	Name  string
	Names map[int64]string
	Order []int64
}

// NewEnum builds an enumeration from alternating value / name pairs in
// definition order.
func NewEnum(name string, pairs ...interface{}) *EnumDef {
	e := &EnumDef{Name: name, Names: map[int64]string{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		v := int64(pairs[i].(int))
		e.Names[v] = pairs[i+1].(string)
		e.Order = append(e.Order, v)
	}
	return e
}

// NameOf returns the name of value or its decimal representation.
func (e *EnumDef) NameOf(v int64) string {
	if n, ok := e.Names[v]; ok {
		return n
	}
	return strconv.FormatInt(v, 10)
}

// Parse accepts a name (case-insensitive) or a number.
func (e *EnumDef) Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
	for v, n := range e.Names {
		if strings.EqualFold(n, s) {
			return v, nil
		}
	}
	if n, err := strconv.ParseInt(s, 0, 64); err == nil {
		if _, ok := e.Names[n]; ok {
			return n, nil
		}
	}
	return 0, invalidValue("%q is not a valid %s (one of %s)", s, e.Name, e.List())
}

// List returns "Name=value, ..." in definition order.
func (e *EnumDef) List() string {
	parts := make([]string, 0, len(e.Order))
	for _, v := range e.Order {
		parts = append(parts, fmt.Sprintf("%s=%d", e.Names[v], v))
	}
	return strings.Join(parts, ", ")
}

// PropSpec describes a single model property (a PyFLP descriptor).
type PropSpec struct {
	Name     string
	Doc      string
	Kind     PropKind
	Enum     *EnumDef
	ReadOnly bool

	get func(m *Model) (interface{}, bool)
	set func(m *Model, v interface{}) error
}

// Model is the common implementation of every PyFLP model class. A model is
// a view over some events (an EventTree) or, for "item" models such as notes,
// over one entry of a list / struct event.
type Model struct {
	// Kind is the model's type name, e.g. "Channel" or "Note".
	Kind string
	// Events is the tree of events the model is a view over (nil for items).
	Events *EventTree

	item   *Container
	parent *Event
	index  int
	kw     map[string]interface{}
	specs  []*PropSpec
}

func newModel(kind string, events *EventTree, specs []*PropSpec) *Model {
	return &Model{Kind: kind, Events: events, specs: specs, kw: map[string]interface{}{}}
}

func newItemModel(kind string, item *Container, index int, parent *Event, specs []*PropSpec) *Model {
	return &Model{Kind: kind, item: item, index: index, parent: parent, specs: specs, kw: map[string]interface{}{}}
}

// Props returns the property descriptors of the model.
func (m *Model) Props() []*PropSpec { return m.specs }

// Spec returns the descriptor for name or nil.
func (m *Model) Spec(name string) *PropSpec {
	for _, s := range m.specs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Get reads a property. ok is false for unknown properties and for
// properties whose underlying event / field is not present (Python: None).
func (m *Model) Get(name string) (interface{}, bool) {
	s := m.Spec(name)
	if s == nil || s.get == nil {
		return nil, false
	}
	return s.get(m)
}

// Set writes a property. v may be a Go value of the property's kind or a
// string, which is parsed according to the kind.
func (m *Model) Set(name string, v interface{}) error {
	s := m.Spec(name)
	if s == nil {
		return fmt.Errorf("%w: %s has no property %q", ErrFLP, m.Kind, name)
	}
	if s.ReadOnly || s.set == nil {
		return fmt.Errorf("%w: property %q of %s is read-only", ErrPropertyCannotBeSet, name, m.Kind)
	}
	cv, err := coerce(s, v)
	if err != nil {
		return err
	}
	return s.set(m, cv)
}

// Typed convenience readers.

func (m *Model) Int(name string) (int64, bool) {
	v, ok := m.Get(name)
	if !ok {
		return 0, false
	}
	n, err := toInt64(v)
	return n, err == nil
}

func (m *Model) Float(name string) (float64, bool) {
	v, ok := m.Get(name)
	if !ok {
		return 0, false
	}
	f, err := toFloat64(v)
	return f, err == nil
}

func (m *Model) Bool(name string) (bool, bool) {
	v, ok := m.Get(name)
	if !ok {
		return false, false
	}
	b, isB := v.(bool)
	return b, isB
}

func (m *Model) Str(name string) (string, bool) {
	v, ok := m.Get(name)
	if !ok {
		return "", false
	}
	s, isS := v.(string)
	return s, isS
}

func (m *Model) Color(name string) (RGBA, bool) {
	v, ok := m.Get(name)
	if !ok {
		return RGBA{}, false
	}
	c, isC := v.(RGBA)
	return c, isC
}

func (m *Model) Pair(name string) ([2]int64, bool) {
	v, ok := m.Get(name)
	if !ok {
		return [2]int64{}, false
	}
	p, isP := v.([2]int64)
	return p, isP
}

// KW returns a value passed when the model was created (e.g. "iid").
func (m *Model) KW(key string) (interface{}, bool) {
	v, ok := m.kw[key]
	return v, ok
}

// ---------------------------------------------------------------------------
// Event / field access helpers used by the specs
// ---------------------------------------------------------------------------

// event returns the first event among ids which exists in the model.
func (m *Model) event(ids ...EventID) *Event {
	if m.Events == nil {
		return nil
	}
	for _, id := range ids {
		if e := m.Events.FirstOf(id); e != nil {
			return e
		}
	}
	return nil
}

func (m *Model) container(id EventID) *Container {
	if m.item != nil {
		return m.item
	}
	e := m.event(id)
	if e == nil {
		return nil
	}
	c, _ := e.Container()
	return c
}

func (m *Model) fieldValue(id EventID, key string) (interface{}, bool) {
	c := m.container(id)
	if c == nil {
		return nil, false
	}
	v, ok := c.Get(key)
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func (m *Model) setFieldValue(id EventID, key string, v interface{}) error {
	if m.item != nil {
		old, exists := m.item.Get(key)
		if !exists || old == nil {
			return cannotSet()
		}
		m.item.Set(key, v)
		if m.parent != nil {
			if err := m.parent.Commit(); err != nil {
				m.item.Set(key, old)
				return err
			}
		}
		return nil
	}
	e := m.event(id)
	if e == nil {
		return cannotSet(id)
	}
	return e.SetField(key, v)
}

func (m *Model) setEventValue(v interface{}, ids ...EventID) error {
	e := m.event(ids...)
	if e == nil {
		return cannotSet(ids...)
	}
	return e.SetValue(v)
}

// ---------------------------------------------------------------------------
// Spec builders
// ---------------------------------------------------------------------------

func withDoc(s *PropSpec, doc string) *PropSpec { s.Doc = doc; return s }

func withEnum(s *PropSpec, e *EnumDef) *PropSpec { s.Kind = KEnum; s.Enum = e; return s }

func readOnly(s *PropSpec) *PropSpec { s.ReadOnly = true; return s }

// evProp is a property bound directly to a fixed size or string event
// (PyFLP's EventProp). The first of ids which exists is used.
func evProp(name string, kind PropKind, doc string, ids ...EventID) *PropSpec {
	return &PropSpec{
		Name: name, Doc: doc, Kind: kind,
		get: func(m *Model) (interface{}, bool) {
			e := m.event(ids...)
			if e == nil {
				return nil, false
			}
			return e.Value(), true
		},
		set: func(m *Model, v interface{}) error { return m.setEventValue(v, ids...) },
	}
}

// evPropDefault is evProp with a default for when the event is missing.
func evPropDefault(name string, kind PropKind, doc string, def interface{}, ids ...EventID) *PropSpec {
	s := evProp(name, kind, doc, ids...)
	inner := s.get
	s.get = func(m *Model) (interface{}, bool) {
		if v, ok := inner(m); ok {
			return v, true
		}
		return def, true
	}
	return s
}

// stProp is a property taken from a field of a struct event (StructProp). For
// item models the id is ignored.
func stProp(name string, kind PropKind, doc string, id EventID, key string) *PropSpec {
	return &PropSpec{
		Name: name, Doc: doc, Kind: kind,
		get: func(m *Model) (interface{}, bool) { return m.fieldValue(id, key) },
		set: func(m *Model, v interface{}) error { return m.setFieldValue(id, key, v) },
	}
}

// itProp is stProp for item models, where the id does not matter.
func itProp(name string, kind PropKind, doc string, key string) *PropSpec {
	return stProp(name, kind, doc, 0, key)
}

// flagProp is a boolean taken from a bit mask. When key is empty the event
// value itself holds the flags, otherwise the struct field named key does.
func flagProp(name, doc string, id EventID, key string, mask int64, inverted bool) *PropSpec {
	read := func(m *Model) (int64, bool) {
		var v interface{}
		var ok bool
		if key == "" {
			e := m.event(id)
			if e == nil {
				return 0, false
			}
			v, ok = e.Value(), true
		} else {
			v, ok = m.fieldValue(id, key)
		}
		if !ok {
			return 0, false
		}
		n, err := toInt64(v)
		return n, err == nil
	}
	return &PropSpec{
		Name: name, Doc: doc, Kind: KBool,
		get: func(m *Model) (interface{}, bool) {
			n, ok := read(m)
			if !ok {
				return nil, false
			}
			set := n&mask == mask
			if inverted {
				set = !set
			}
			return set, true
		},
		set: func(m *Model, v interface{}) error {
			b := v.(bool)
			if inverted {
				b = !b
			}
			n, ok := read(m)
			if !ok {
				return cannotSet(id)
			}
			if b {
				n |= mask
			} else {
				n &^= mask
			}
			if key == "" {
				return m.setEventValue(n, id)
			}
			return m.setFieldValue(id, key, n)
		},
	}
}

// customProp builds a property from closures.
func customProp(name string, kind PropKind, doc string,
	get func(m *Model) (interface{}, bool), set func(m *Model, v interface{}) error) *PropSpec {
	return &PropSpec{Name: name, Doc: doc, Kind: kind, get: get, set: set, ReadOnly: set == nil}
}

// kwProp exposes a value passed to the model constructor (PyFLP's KWProp).
func kwProp(name string, kind PropKind, doc, key string, writable bool) *PropSpec {
	s := &PropSpec{
		Name: name, Doc: doc, Kind: kind, ReadOnly: !writable,
		get: func(m *Model) (interface{}, bool) {
			v, ok := m.kw[key]
			return v, ok && v != nil
		},
	}
	if writable {
		s.set = func(m *Model, v interface{}) error {
			if _, ok := m.kw[key]; !ok {
				return fmt.Errorf("%w: %s", ErrPropertyCannotBeSet, key)
			}
			m.kw[key] = v
			return nil
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Coercion of user supplied values
// ---------------------------------------------------------------------------

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "t", "yes", "y", "on", "1":
		return true, nil
	case "false", "f", "no", "n", "off", "0":
		return false, nil
	}
	return false, invalidValue("%q is not a boolean", s)
}

func parsePair(s string) ([2]int64, error) {
	s = strings.Trim(strings.TrimSpace(s), "()[]")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ':' || r == ' ' })
	if len(parts) != 2 {
		return [2]int64{}, invalidValue("%q is not a pair (expected \"a,b\")", s)
	}
	a, err := strconv.ParseInt(parts[0], 0, 64)
	if err != nil {
		return [2]int64{}, invalidValue("%q is not an integer", parts[0])
	}
	b, err := strconv.ParseInt(parts[1], 0, 64)
	if err != nil {
		return [2]int64{}, invalidValue("%q is not an integer", parts[1])
	}
	return [2]int64{a, b}, nil
}

func parseMusical(s string) (MusicalTime, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return MusicalTime{}, invalidValue("%q is not a musical time (expected bars:beats:ticks)", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return MusicalTime{}, invalidValue("%q is not an integer", p)
		}
		n[i] = v
	}
	return MusicalTime{n[0], n[1], n[2]}, nil
}

func coerce(s *PropSpec, v interface{}) (interface{}, error) {
	switch s.Kind {
	case KInt:
		if str, ok := v.(string); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(str), 0, 64)
			if err != nil {
				return nil, invalidValue("%q is not an integer", str)
			}
			return n, nil
		}
		return toInt64(v)
	case KFloat:
		if str, ok := v.(string); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
			if err != nil {
				return nil, invalidValue("%q is not a number", str)
			}
			return f, nil
		}
		return toFloat64(v)
	case KBool:
		if str, ok := v.(string); ok {
			return parseBool(str)
		}
		return toBool(v)
	case KString:
		if str, ok := v.(string); ok {
			return str, nil
		}
		return fmt.Sprint(v), nil
	case KColor:
		switch x := v.(type) {
		case RGBA:
			return x, nil
		case string:
			return ParseRGBA(x)
		}
	case KEnum:
		if str, ok := v.(string); ok {
			if s.Enum == nil {
				return nil, invalidValue("%q: enum has no names", str)
			}
			return s.Enum.Parse(str)
		}
		n, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		if s.Enum != nil {
			if _, ok := s.Enum.Names[n]; !ok {
				return nil, invalidValue("%d is not a valid %s (one of %s)", n, s.Enum.Name, s.Enum.List())
			}
		}
		return n, nil
	case KPair:
		if str, ok := v.(string); ok {
			return parsePair(str)
		}
		return toPair(v)
	case KMusical:
		switch x := v.(type) {
		case MusicalTime:
			return x, nil
		case string:
			return parseMusical(x)
		}
	case KBytes:
		switch x := v.(type) {
		case []byte:
			return x, nil
		case string:
			b, err := hex.DecodeString(strings.TrimSpace(x))
			if err != nil {
				return nil, invalidValue("invalid hex: %v", err)
			}
			return b, nil
		}
	case KVersion:
		switch x := v.(type) {
		case FLVersion:
			return x, nil
		case string:
			return ParseFLVersion(x)
		case []int:
			if len(x) >= 1 && len(x) <= 4 {
				return NewFLVersion(x[0], x[1:]...), nil
			}
		}
	case KDateTime, KDuration, KStrMap:
		return v, nil
	}
	return nil, invalidValue("cannot use %T for property %q (%s)", v, s.Name, s.Kind)
}

// ---------------------------------------------------------------------------
// Formatting / dumping
// ---------------------------------------------------------------------------

// FormatValue converts a property value into something JSON friendly and
// human readable: enums become their names, colours become hex strings etc.
// Large byte blobs are summarised unless full is true.
func FormatValue(s *PropSpec, v interface{}, full bool) interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case RGBA:
		return x.Hex()
	case MusicalTime:
		return x.String()
	case FLVersion:
		return x.String()
	case [2]int64:
		return []int64{x[0], x[1]}
	case []byte:
		if full || len(x) <= 32 {
			return hex.EncodeToString(x)
		}
		return fmt.Sprintf("<%d bytes>", len(x))
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	case time.Duration:
		return x.String()
	case map[int64]string:
		keys := make([]int64, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		om := &OrderedMap{}
		for _, k := range keys {
			om.Set(strconv.FormatInt(k, 10), x[k])
		}
		return om
	case int64:
		if s != nil && s.Kind == KEnum && s.Enum != nil {
			return s.Enum.NameOf(x)
		}
	}
	return v
}

// OrderedMap is a string keyed map which keeps insertion order when
// marshalled to JSON.
type OrderedMap struct {
	Keys []string
	Vals map[string]interface{}
}

// NewOrderedMap returns an empty OrderedMap.
func NewOrderedMap() *OrderedMap { return &OrderedMap{} }

// Set adds or replaces a key.
func (o *OrderedMap) Set(k string, v interface{}) {
	if o.Vals == nil {
		o.Vals = map[string]interface{}{}
	}
	if _, ok := o.Vals[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Vals[k] = v
}

// MarshalJSON implements json.Marshaler.
func (o *OrderedMap) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(o.Vals[k])
		if err != nil {
			return nil, err
		}
		sb.Write(kb)
		sb.WriteByte(':')
		sb.Write(vb)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

// Dump returns all present properties of the model, formatted for display.
func (m *Model) Dump(full bool) *OrderedMap {
	out := NewOrderedMap()
	for _, s := range m.specs {
		v, ok := s.get(m)
		if !ok {
			continue
		}
		out.Set(s.Name, FormatValue(s, v, full))
	}
	return out
}

func (m *Model) String() string {
	d := m.Dump(false)
	parts := make([]string, 0, len(d.Keys))
	for _, k := range d.Keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, d.Vals[k]))
	}
	return fmt.Sprintf("%s(%s)", m.Kind, strings.Join(parts, ", "))
}
