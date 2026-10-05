package pyflp

// Remote ("internal") controllers link a plugin parameter to a controller.
//
// Note: PyFLP evaluates `value := x["parameter_data"] is not None` with the
// walrus operator binding the *boolean*, so its `parameter` and `controls_vst`
// always return 0/1 style results. This port implements the documented intent.

func remoteParam(m *Model) (int64, bool) {
	return m.Int("parameter_data")
}

var remoteControllerSpecs = []*PropSpec{
	readOnly(customProp("parameter", KInt, "The ID of the plugin parameter the controller is linked to.",
		func(m *Model) (interface{}, bool) {
			v, ok := m.fieldValue(IDControllerRemote, "parameter_data")
			if !ok {
				return nil, false
			}
			n, _ := toInt64(v)
			return n & 0x7FFF, true
		}, nil)),
	readOnly(customProp("controls_vst", KBool, "Whether parameter is linked to a VST plugin (false for insert slot parameters).",
		func(m *Model) (interface{}, bool) {
			v, ok := m.fieldValue(IDControllerRemote, "parameter_data")
			if !ok {
				return nil, false
			}
			n, _ := toInt64(v)
			return n&0x8000 > 0, true
		}, nil)),
	stProp("parameter_data", KInt, "Raw parameter data (bit 15 = VST flag).", IDControllerRemote, "parameter_data"),
	stProp("destination_data", KInt, "Raw destination data.", IDControllerRemote, "destination_data"),
}

// RemoteControllers returns the remote controllers found in events.
func RemoteControllers(events *EventTree) []*Model {
	var out []*Model
	for _, et := range events.Separate(IDControllerRemote) {
		out = append(out, newModel("RemoteController", et, remoteControllerSpecs))
	}
	return out
}

// MIDIControllerEvents returns the raw MIDI controller events of events.
func MIDIControllerEvents(events *EventTree) []*Event {
	return events.Get(IDControllerMIDI)
}
