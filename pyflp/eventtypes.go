package pyflp

import (
	"fmt"
)

// Struct events carry the payload length as context (some layouts depend on
// it); list events are made of repeated structures.

func lenCtx(n int) Ctx { return Ctx{Len: n} }

func structEvent(name string, fields ...NamedField) *EventType {
	return &EventType{Name: name, Kind: KindStruct, codec: st(fields...), ctx: lenCtx}
}

func listEvent(name string, sub Field) *EventType {
	return &EventType{Name: name, Kind: KindList, codec: greedyRangeField{sub}, ctx: lenCtx}
}

// enumStr maps integers to names (construct.Enum). Unknown values are kept
// as plain integers so that they survive a round trip.
func enumStr(sub Field, names map[int64]string) Field {
	rev := make(map[string]int64, len(names))
	for k, v := range names {
		rev[v] = k
	}
	return adaptField{
		sub: sub,
		dec: func(v interface{}) (interface{}, error) {
			n, err := toInt64(v)
			if err != nil {
				return nil, err
			}
			if s, ok := names[n]; ok {
				return s, nil
			}
			return n, nil
		},
		enc: func(v interface{}) (interface{}, error) {
			switch x := v.(type) {
			case string:
				if n, ok := rev[x]; ok {
					return n, nil
				}
				return nil, invalidValue("%q is not one of the allowed values", x)
			default:
				return toInt64(v)
			}
		},
	}
}

func ifNew(f Field) Field {
	return ifField{cond: func(c *Ctx, cur *Container) bool { return c.New }, sub: f}
}

// ---------------------------------------------------------------------------
// project / arrangement / timemarker / controller
// ---------------------------------------------------------------------------

var (
	TimestampEvent = structEvent("TimestampEvent",
		nf("created_on", fF64), nf("time_spent", fF64))

	PLSelectionEvent = structEvent("PLSelectionEvent",
		nf("start", opt(fU32)), nf("end", opt(fU32)))

	PlaylistEvent = &EventType{
		Name: "PlaylistEvent", Kind: KindList,
		ctx: func(n int) Ctx { return Ctx{Len: n, New: n%60 == 0} },
		codec: greedyRangeField{st(
			nf("position", fU32),
			nf("pattern_base", fU16), // always 20480
			nf("item_index", fU16),
			nf("length", fU32),
			nf("track_rvidx", fU16), // stored reversed, i.e. track 1 is 499
			nf("group", fU16),
			nf("_u1", fBytes(2)),
			nf("item_flags", fU16),
			nf("_u2", fBytes(4)),
			nf("start_offset", fF32),
			nf("end_offset", fF32),
			nf("_u3", ifNew(fBytes(28))), // new in FL 21
		)},
	}

	TrackEvent = structEvent("TrackEvent",
		nf("iid", opt(fU32)),
		nf("color", opt(fU32)),
		nf("icon", opt(fU32)),
		nf("enabled", opt(fFlag)),
		nf("height", opt(heightField)),
		nf("locked_height", opt(fI32)),
		nf("content_locked", opt(fFlag)),
		nf("motion", opt(fU32)),
		nf("press", opt(fU32)),
		nf("trigger_sync", opt(fU32)),
		nf("queued", opt(fourByteBool)),
		nf("tolerant", opt(fourByteBool)),
		nf("position_sync", opt(fU32)),
		nf("grouped", opt(fFlag)),
		nf("locked", opt(fFlag)),
		nf("_u1", opt(fGreed)),
	)

	MIDIControllerEvent = structEvent("MIDIControllerEvent", nf("_u1", fGreed))

	RemoteControllerEvent = structEvent("RemoteControllerEvent",
		nf("_u1", opt(fBytes(2))),
		nf("_u2", opt(fU8)),
		nf("_u3", opt(fU8)),
		nf("parameter_data", opt(fU16)),
		nf("destination_data", opt(fI16)),
		nf("_u4", opt(fBytes(8))),
		nf("_u5", opt(fBytes(4))),
	)
)

// ---------------------------------------------------------------------------
// patterns
// ---------------------------------------------------------------------------

var (
	ControllerEvent = listEvent("ControllerEvent", st(
		nf("position", fU32), // can be a delta as well
		nf("_u1", fU8),
		nf("_u2", fU8),
		nf("channel", fU8),
		nf("_flags", fU8),
		nf("value", fF32),
	))

	NotesEvent = listEvent("NotesEvent", st(
		nf("position", fU32),
		nf("flags", fU16),
		nf("rack_channel", fU16),
		nf("length", fU32),
		nf("key", fU16),
		nf("group", fU16),
		nf("fine_pitch", fU8),
		nf("_u1", fU8),
		nf("release", fU8),
		nf("midi_channel", fU8),
		nf("pan", fU8),
		nf("velocity", fU8),
		nf("mod_x", fU8),
		nf("mod_y", fU8),
	))
)

// ---------------------------------------------------------------------------
// channels
// ---------------------------------------------------------------------------

var (
	AutomationEvent = structEvent("AutomationEvent",
		nf("_u1", fBytes(4)),
		nf("lfo.amount", fI32),
		nf("_u2", fBytes(1)),
		nf("_u3", fBytes(2)),
		nf("_u4", fBytes(2)),
		nf("_u5", fBytes(4)),
		nf("points", automationPointsField{}),
		nf("_u6", fGreed),
	)

	DelayEvent = structEvent("DelayEvent",
		nf("feedback", opt(fU32)),
		nf("pan", opt(fI32)),
		nf("pitch_shift", opt(fI32)),
		nf("echoes", opt(fU32)),
		nf("time", opt(fU32)),
	)

	EnvelopeLFOEvent = structEvent("EnvelopeLFOEvent",
		nf("flags", opt(fI32)),
		nf("envelope.enabled", opt(fI32)),
		nf("envelope.predelay", opt(fI32)),
		nf("envelope.attack", opt(fI32)),
		nf("envelope.hold", opt(fI32)),
		nf("envelope.decay", opt(fI32)),
		nf("envelope.sustain", opt(fI32)),
		nf("envelope.release", opt(fI32)),
		nf("envelope.amount", opt(fI32)),
		nf("lfo.predelay", opt(fU32)),
		nf("lfo.attack", opt(fU32)),
		nf("lfo.amount", opt(fI32)),
		nf("lfo.speed", opt(fU32)),
		nf("lfo.shape", opt(fI32)),
		nf("envelope.attack_tension", opt(fI32)),
		nf("envelope.decay_tension", opt(fI32)),
		nf("envelope.release_tension", opt(fI32)),
	)

	LevelAdjustsEvent = structEvent("LevelAdjustsEvent",
		nf("pan", opt(fI32)),
		nf("volume", opt(fU32)),
		nf("_u1", opt(fU32)),
		nf("mod_x", opt(fI32)),
		nf("mod_y", opt(fI32)),
	)

	LevelsEvent = structEvent("LevelsEvent",
		nf("pan", opt(fI32)),
		nf("volume", opt(fU32)),
		nf("pitch_shift", opt(fI32)),
		nf("filter.mod_x", opt(fU32)),
		nf("filter.mod_y", opt(fU32)),
		nf("filter.type", opt(fU32)),
	)

	ParametersEvent = structEvent("ParametersEvent",
		nf("_u1", opt(fBytes(9))),
		nf("fx.remove_dc", opt(fFlag)),
		nf("delay.flags", opt(fU8)),
		nf("keyboard.main_pitch", opt(fFlag)),
		nf("_u2", opt(fBytes(28))),
		nf("arp.direction", opt(fU32)),
		nf("arp.range", opt(fU32)),
		nf("arp.chord", opt(fU32)),
		nf("arp.time", opt(fF32)),
		nf("arp.gate", opt(fF32)),
		nf("arp.slide", opt(fFlag)),
		nf("_u3", opt(fBytes(1))),
		nf("time.full_porta", opt(fFlag)),
		nf("keyboard.add_root", opt(fFlag)),
		nf("time.gate", opt(fU16)),
		nf("_u4", opt(fBytes(2))),
		nf("keyboard.key_region", opt(pairOf(fU32))),
		nf("_u5", opt(fBytes(4))),
		nf("fx.normalize", opt(fFlag)),
		nf("fx.inverted", opt(fFlag)),
		nf("_u6", opt(fBytes(1))),
		nf("content.declick_mode", opt(fU8)),
		nf("fx.crossfade", opt(fU32)),
		nf("fx.trim", opt(fU32)),
		nf("arp.repeat", opt(fU32)),
		nf("stretching.time", opt(linearMusical)),
		nf("stretching.pitch", opt(fI32)),
		nf("stretching.multiplier", opt(log2Field(fI32, 10000))),
		nf("stretching.mode", opt(fI32)),
		nf("_u7", opt(fBytes(21))),
		nf("fx.start", opt(logNormal(arr(2, fU16), 0, 61440))),
		nf("_u8", opt(fBytes(4))),
		nf("fx.length", opt(logNormal(arr(2, fU16), 0, 61440))),
		nf("_u9", opt(fBytes(3))),
		nf("playback.start_offset", opt(fU32)),
		nf("_u10", opt(fBytes(5))),
		nf("fx.fix_trim", opt(fFlag)),
		nf("_extra", fGreed),
	)

	PolyphonyEvent = structEvent("PolyphonyEvent",
		nf("max", opt(fU32)),
		nf("slide", opt(fU32)),
		nf("flags", opt(fU8)),
	)

	TrackingEvent = structEvent("TrackingEvent",
		nf("middle_value", opt(fU32)),
		nf("pan", opt(fI32)),
		nf("mod_x", opt(fI32)),
		nf("mod_y", opt(fI32)),
	)
)

// ---------------------------------------------------------------------------
// mixer
// ---------------------------------------------------------------------------

var (
	InsertFlagsEvent = structEvent("InsertFlagsEvent",
		nf("_u1", opt(fBytes(4))),
		nf("flags", opt(fU32)),
		nf("_u2", opt(fBytes(4))),
	)

	// InsertRoutingEvent holds one bool per insert (value type []bool).
	InsertRoutingEvent = &EventType{Name: "InsertRoutingEvent", Kind: KindList, codec: greedyFlagsField{}, ctx: lenCtx}

	MixerParamsEvent = listEvent("MixerParamsEvent", st(
		nf("_u4", fBytes(4)),
		nf("id", fU8),
		nf("_u1", fU8),
		nf("channel_data", fU16),
		nf("msg", fI32),
	))
)

// ---------------------------------------------------------------------------
// plugins
// ---------------------------------------------------------------------------

var (
	WrapperEvent = structEvent("WrapperEvent",
		nf("_u1", opt(fBytes(16))),
		nf("flags", opt(fU16)),
		nf("_u2", opt(fBytes(2))),
		nf("page", opt(fU8)),
		nf("_u3", opt(fBytes(23))),
		nf("width", opt(fU32)),
		nf("height", opt(fU32)),
		nf("_extra", fGreed),
	)

	BooBassEvent = structEvent("BooBassEvent",
		nf("_u1", ifLen(16, fBytes(4))),
		nf("bass", fU32), nf("mid", fU32), nf("high", fU32),
	)

	FruitKickEvent = structEvent("FruitKickEvent",
		nf("_u1", fBytes(4)),
		nf("max_freq", fI32), nf("min_freq", fI32),
		nf("freq_decay", fU32), nf("amp_decay", fU32),
		nf("click", fU32), nf("distortion", fU32),
		nf("_u2", fBytes(4)),
	)

	FruityBalanceEvent = structEvent("FruityBalanceEvent",
		nf("pan", fU32), nf("volume", fU32))

	FruityBloodOverdriveEvent = structEvent("FruityBloodOverdriveEvent",
		nf("plugin_marker", ifLen(36, fBytes(4))),
		nf("pre_band", fU32), nf("color", fU32), nf("pre_amp", fU32),
		nf("x100", fourByteBool),
		nf("post_filter", fU32), nf("post_gain", fU32),
		nf("_u1", fBytes(4)), nf("_u2", fBytes(4)),
	)

	FruityCenterEvent = structEvent("FruityCenterEvent",
		nf("_u1", ifLen(8, fBytes(4))), nf("enabled", fourByteBool))

	FruityFastDistEvent = structEvent("FruityFastDistEvent",
		nf("pre", fU32), nf("threshold", fU32),
		nf("kind", enumStr(fU32, map[int64]string{0: "A", 1: "B"})),
		nf("mix", fU32), nf("post", fU32),
	)

	FruityNotebook2Event = structEvent("FruityNotebook2Event",
		nf("_u1", fBytes(4)),
		nf("active_page", fU32),
		nf("pages", notebookPagesField{}),
		nf("editable", fFlag),
	)

	FruitySendEvent = structEvent("FruitySendEvent",
		nf("pan", fI32), nf("dry", fU32), nf("volume", fU32), nf("send_to", fI32))

	FruitySoftClipperEvent = structEvent("FruitySoftClipperEvent",
		nf("threshold", fU32), nf("post", fU32))

	FruityStereoEnhancerEvent = structEvent("FruityStereoEnhancerEvent",
		nf("pan", fI32), nf("volume", fU32),
		nf("stereo_separation", fU32), nf("phase_offset", fU32),
		nf("effect_position", enumStr(fU32, map[int64]string{0: "pre", 1: "post"})),
		nf("phase_inversion", enumStr(fU32, map[int64]string{0: "none", 1: "left", 2: "right"})),
	)

	PluckedEvent = structEvent("PluckedEvent",
		nf("decay", fU32), nf("color", fU32),
		nf("normalize", fourByteBool), nf("gate", fourByteBool), nf("widen", fourByteBool))

	SoundgoodizerEvent = structEvent("SoundgoodizerEvent",
		nf("_u1", ifLen(12, fBytes(4))),
		nf("mode", enumStr(fU32, map[int64]string{0: "A", 1: "B", 2: "C", 3: "D"})),
		nf("amount", fU32),
	)
)

// VST plugin event ids (pyflp.plugin._VSTPluginEventID).
const (
	VSTMIDI       = 1
	VSTFlags      = 2
	VSTIO         = 30
	VSTInputs     = 31
	VSTOutputs    = 32
	VSTPluginInfo = 50
	VSTFourCC     = 51
	VSTGUID       = 52
	VSTState      = 53
	VSTName       = 54
	VSTPluginPath = 55
	VSTVendor     = 56
	VST57         = 57
)

var vstEventNames = map[int64]string{
	VSTMIDI: "MIDI", VSTFlags: "Flags", VSTIO: "IO", VSTInputs: "Inputs",
	VSTOutputs: "Outputs", VSTPluginInfo: "PluginInfo", VSTFourCC: "FourCC",
	VSTGUID: "GUID", VSTState: "State", VSTName: "Name",
	VSTPluginPath: "PluginPath", VSTVendor: "Vendor", VST57: "_57",
}

var vstMIDIStruct = st(
	nf("input", opt(fI32)),
	nf("output", opt(fI32)),
	nf("pb_range", opt(fU32)),
	nf("_extra", fGreed),
)

var vstFlagsStruct = st(
	nf("_u1", opt(fBytes(9))),
	nf("flags", opt(fU32)),
	nf("flags2", opt(fU32)),
	nf("_u2", opt(fBytes(5))),
	nf("fast_idle", opt(fFlag)),
	nf("_extra", fGreed),
)

var vstText = greedyStringField{encUTF8}

// VSTPluginEvent is the data event of the "Fruity Wrapper": a type marker
// followed by a list of (id, data) sub events.
var VSTPluginEvent = &EventType{
	Name: "VSTPluginEvent",
	Kind: KindStruct,
	ctx:  lenCtx,
	check: func(data []byte) {
		if len(data) > 0 && data[0] != 8 && data[0] != 10 {
			warn("VSTPluginEvent: Unknown marker %d detected. Open an issue at "+
				"https://github.com/demberto/PyFLP/issues if you are seeing this!", data[0])
		}
	},
	codec: st(
		nf("type", fU32), // 8 or 10 for VSTs
		nf("events", greedyRangeField{st(
			nf("id", fU32),
			nf("data", prefixedField{
				lenF: fU64,
				sub: switchField{
					key: "id",
					cases: map[int64]Field{
						VSTMIDI:       vstMIDIStruct,
						VSTFlags:      vstFlagsStruct,
						VSTFourCC:     vstText,
						VSTName:       vstText,
						VSTVendor:     vstText,
						VSTPluginPath: vstText,
					},
					def: fGreed,
				},
			}),
		)}),
	),
}

// pluginTypes maps FL's internal plugin names to the type of their data
// event (pyflp.plugin.get_event_by_internal_name).
var pluginTypes = map[string]*EventType{
	"Fruity Wrapper":         VSTPluginEvent,
	"BooBass":                BooBassEvent,
	"Fruit Kick":             FruitKickEvent,
	"Fruity Balance":         FruityBalanceEvent,
	"Fruity Blood Overdrive": FruityBloodOverdriveEvent,
	"Fruity Center":          FruityCenterEvent,
	"Fruity Fast Dist":       FruityFastDistEvent,
	"Fruity NoteBook 2":      FruityNotebook2Event,
	"Fruity Send":            FruitySendEvent,
	"Fruity Soft Clipper":    FruitySoftClipperEvent,
	"Fruity Stereo Enhancer": FruityStereoEnhancerEvent,
	"Plucked!":               PluckedEvent,
	"Soundgoodizer":          SoundgoodizerEvent,
}

// EventTypeByInternalName returns the event type used by a plugin's data
// event, or NativePluginEvent for plugins which are not implemented.
func EventTypeByInternalName(name string) *EventType {
	if t, ok := pluginTypes[name]; ok {
		return t
	}
	return NativePluginEvent
}

// PluginInternalNames lists the internal names of all known plugins.
func PluginInternalNames() []string {
	return []string{
		"BooBass", "Fruit Kick", "Fruity Balance", "Fruity Blood Overdrive",
		"Fruity Center", "Fruity Fast Dist", "Fruity NoteBook 2", "Fruity Send",
		"Fruity Soft Clipper", "Fruity Stereo Enhancer", "Fruity Wrapper",
		"Plucked!", "Soundgoodizer",
	}
}

func vstEventName(id int64) string {
	if n, ok := vstEventNames[id]; ok {
		return n
	}
	return fmt.Sprintf("%d", id)
}
