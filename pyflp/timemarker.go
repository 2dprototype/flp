package pyflp

import "fmt"

var timeMarkerSpecs = []*PropSpec{
	evProp("denominator", KInt, "Denominator of a time signature marker.", IDTimeMarkerDenominator),
	evProp("name", KString, "Name of the marker.", IDTimeMarkerName),
	evProp("numerator", KInt, "Numerator of a time signature marker.", IDTimeMarkerNumerator),
	readOnly(customProp("position", KInt, "Position of the marker (PPQ dependant).", func(m *Model) (interface{}, bool) {
		e := m.event(IDTimeMarkerPosition)
		if e == nil {
			return nil, false
		}
		v, _ := e.Int()
		if v < TimeMarkerTypeSignature {
			return v, true
		}
		return v - TimeMarkerTypeSignature, true
	}, nil)),
	readOnly(withEnum(customProp("type", KEnum, "The action with which a time marker is associated.", func(m *Model) (interface{}, bool) {
		e := m.event(IDTimeMarkerPosition)
		if e == nil {
			return nil, false
		}
		v, _ := e.Int()
		if v >= TimeMarkerTypeSignature {
			return int64(TimeMarkerTypeSignature), true
		}
		return int64(TimeMarkerTypeMarker), true
	}, nil), TimeMarkerTypeEnum)),
}

// TimeMarker is a marker or time signature in the timeline of an
// arrangement or pattern.
type TimeMarker struct{ *Model }

func newTimeMarker(t *EventTree) *TimeMarker {
	return &TimeMarker{newModel("TimeMarker", t, timeMarkerSpecs)}
}

// String describes the marker the way PyFLP does.
func (t *TimeMarker) String() string {
	name, _ := t.Str("name")
	pos, _ := t.Int("position")
	typ, _ := t.Int("type")
	if typ == TimeMarkerTypeMarker {
		if name != "" {
			return fmt.Sprintf("Marker %q @ %d", name, pos)
		}
		return fmt.Sprintf("Unnamed marker @ %d", pos)
	}
	num, _ := t.Int("numerator")
	den, _ := t.Int("denominator")
	sig := fmt.Sprintf("%d/%d", num, den)
	if name != "" {
		return fmt.Sprintf("Signature %q (%s) @ %d", name, sig, pos)
	}
	return fmt.Sprintf("Unnamed %s signature @ %d", sig, pos)
}

func timeMarkersOf(t *EventTree) []*TimeMarker {
	var out []*TimeMarker
	for _, ed := range t.Group(IDTimeMarkerNumerator, IDTimeMarkerDenominator, IDTimeMarkerPosition, IDTimeMarkerName) {
		out = append(out, newTimeMarker(ed))
	}
	return out
}
