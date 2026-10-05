package pyflp

import (
	"fmt"
)

// ---------------------------------------------------------------------------
// Wrapper properties shared by every plugin
// ---------------------------------------------------------------------------

func wrapperFlag(name, doc string, mask int64) *PropSpec {
	return flagProp(name, doc, IDPluginWrapper, "flags", mask, false)
}

func basePluginSpecs() []*PropSpec {
	return []*PropSpec{
		wrapperFlag("compact", "Whether the plugin page toolbar (Detailed settings) is hidden.", WrapperHideSettings),
		wrapperFlag("demo_mode", "Whether the plugin state was saved in a demo / trial version.", WrapperDemoMode),
		wrapperFlag("detached", "Plugin editor can be moved between monitors when detached.", WrapperDetached),
		wrapperFlag("disabled", "Legacy property; check Channel.enabled or Slot.enabled instead.", WrapperDisabled),
		wrapperFlag("directx", "Whether the plugin is a DirectX plugin.", WrapperDirectX),
		wrapperFlag("generator", "Whether the plugin is a generator or an effect.", WrapperGenerator),
		stProp("height", KInt, "Height of the plugin editor (in pixels).", IDPluginWrapper, "height"),
		wrapperFlag("minimized", "Whether the plugin editor is minimized.", WrapperMinimized),
		wrapperFlag("multithreaded", "Whether threaded processing is enabled.", WrapperThreadedProcessing),
		withEnum(stProp("page", KEnum, "Active / selected page.", IDPluginWrapper, "page"), WrapperPageEnum),
		wrapperFlag("smart_disable", "Whether smart disable is enabled.", WrapperSmartDisable),
		wrapperFlag("visible", "Whether the editor of the plugin is visible.", WrapperVisible),
		stProp("width", KInt, "Width of the plugin editor (in pixels).", IDPluginWrapper, "width"),
	}
}

// np is a property of a native plugin's data event.
func np(name string, kind PropKind, doc, key string) *PropSpec {
	return stProp(name, kind, doc, IDPluginData, key)
}

// ---------------------------------------------------------------------------
// VST plugin properties (stored in sub events of the data event)
// ---------------------------------------------------------------------------

// vstEntry finds the sub event with the id inside the VST data event.
func vstEntry(m *Model, id int64) (*Event, *Container) {
	ev := m.event(IDPluginData)
	if ev == nil {
		return nil, nil
	}
	c, ok := ev.Container()
	if !ok {
		return nil, nil
	}
	list, ok := c.List("events")
	if !ok {
		return nil, nil
	}
	for _, e := range list {
		if v, ok := e.Int("id"); ok && v == id {
			return ev, e
		}
	}
	return nil, nil
}

// vstProp reads a field of a sub event; key == "" means the sub event's whole
// data (a string or bytes).
func vstProp(name string, kind PropKind, doc string, subID int64, key string) *PropSpec {
	return &PropSpec{
		Name: name, Doc: doc, Kind: kind,
		get: func(m *Model) (interface{}, bool) {
			_, e := vstEntry(m, subID)
			if e == nil {
				return nil, false
			}
			data, _ := e.Get("data")
			if key == "" {
				return data, data != nil
			}
			c, ok := data.(*Container)
			if !ok {
				return nil, false
			}
			v, ok := c.Get(key)
			return v, ok && v != nil
		},
		set: func(m *Model, v interface{}) error {
			ev, e := vstEntry(m, subID)
			if e == nil {
				return cannotSet(IDPluginData)
			}
			if key == "" {
				old, _ := e.Get("data")
				e.Set("data", v)
				if err := ev.Commit(); err != nil {
					e.Set("data", old)
					return err
				}
				return nil
			}
			c, ok := e.Sub("data")
			if !ok {
				return cannotSet(IDPluginData)
			}
			old, exists := c.Get(key)
			if !exists || old == nil {
				return cannotSet(IDPluginData)
			}
			c.Set(key, v)
			if err := ev.Commit(); err != nil {
				c.Set(key, old)
				return err
			}
			return nil
		},
	}
}

func vstFlag(name, doc string, key string, mask int64, inverted bool) *PropSpec {
	base := vstProp(name, KBool, doc, VSTFlags, key)
	read := base.get
	return &PropSpec{
		Name: name, Doc: doc, Kind: KBool,
		get: func(m *Model) (interface{}, bool) {
			v, ok := read(m)
			if !ok {
				return nil, false
			}
			n, err := toInt64(v)
			if err != nil {
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
			cur, ok := read(m)
			if !ok {
				return cannotSet(IDPluginData)
			}
			n, _ := toInt64(cur)
			if b {
				n |= mask
			} else {
				n &^= mask
			}
			return base.set(m, n)
		},
	}
}

var (
	vstAutomationSpecs = []*PropSpec{
		vstFlag("notify_changes", "Record parameter changes as automation (Notify about parameter changes). Defaults to true.",
			"flags", VSTDontNotifyChanges, true),
	}

	vstCompatibilitySpecs = []*PropSpec{
		vstFlag("buffers_maxsize", "Use maximum buffer size from host. Defaults to false.", "flags2", VST2UseMaxFromHost, false),
		vstProp("fast_idle", KBool, "Increases idle rate; can make the plugin GUI more responsive. Defaults to false.", VSTFlags, "fast_idle"),
		vstFlag("fixed_buffers", "Use fixed size buffers. Defaults to false.", "flags", VSTFixedSizeBuffers, false),
		vstFlag("process_maximum", "Process maximum size buffers. Defaults to false.", "flags2", VST2ProcessMaxSize, false),
		vstFlag("reset_on_transport", "Reset plugin when FL Studio resets. Defaults to true.", "flags", VSTDontResetOnTransport, true),
		vstFlag("send_loop", "Lets the plugin know about the loop position. Defaults to true.", "flags", VSTSendLoopPos, false),
		vstFlag("use_time_offset", "Adjust time information reported by the plugin. Defaults to false.", "flags", VSTDontUseTimeOffset, true),
	}

	vstMIDISpecs = []*PropSpec{
		vstProp("input", KInt, "MIDI input port. Min = 0, Max = 255. Not selected = -1 (default).", VSTMIDI, "input"),
		vstProp("output", KInt, "MIDI output port. Min = 0, Max = 255. Not selected = -1 (default).", VSTMIDI, "output"),
		vstProp("pb_range", KInt, "Pitch bend range sent to the plugin (semitones). Min = 1, Max = 48, default 12.", VSTMIDI, "pb_range"),
		vstFlag("send_modx", "Send MOD X as polyphonic aftertouch. Defaults to false.", "flags", VSTSendModX, false),
		vstFlag("send_pb", "Send pitch bend range (semitones). Defaults to false.", "flags", VSTSendPBRange, false),
		vstFlag("send_release", "Send note release velocity. Defaults to true.", "flags", VSTDontSendRelVelo, true),
	}

	vstProcessingSpecs = []*PropSpec{
		vstFlag("allow_sd", "Allow smart disable. Defaults to true.", "flags", VSTAllowSmartDisable, false),
		vstFlag("bridged", "Load a plugin in a separate process (Make bridged). Defaults to false.", "flags", VSTLoadBridged, false),
		vstFlag("external", "Keep plugin editor in the bridge process. Defaults to false.", "flags", VSTExternalWindow, false),
		vstFlag("keep_state", "Ensure processor state in callbacks. Defaults to true.", "flags", VSTDontKeepCPUState, true),
		vstFlag("multithreaded", "Allow threaded processing. Defaults to true.", "flags", VSTAllowThreaded, false),
		vstFlag("notify_render", "Notify about rendering mode. Defaults to true.", "flags", VSTNotifyRender, false),
		vstFlag("process_inactive", "Process inactive inputs and outputs. Defaults to true.", "flags", VSTProcessInactive, false),
	}

	vstUISpecs = []*PropSpec{
		vstFlag("accept_drop", "Accept dropped files. Defaults to false.", "flags", VSTAcceptFileDrop, false),
		vstFlag("always_update", "Whether the plugin UI should be updated when hidden. Defaults to false.", "flags", VSTUpdateWhenHidden, false),
		vstFlag("dpi_aware", "DPI aware when bridged. Defaults to true.", "flags", VSTDPIAwareBridged, false),
		vstFlag("scale_editor", "Scale editor dimensions. Defaults to false.", "flags", VSTScaleEditor, false),
	}

	vstOwnSpecs = []*PropSpec{
		vstProp("fourcc", KString, "A unique four character code identifying the plugin.", VSTFourCC, ""),
		vstProp("guid", KBytes, "The plugin's GUID (raw bytes).", VSTGUID, ""),
		vstProp("name", KString, "Factory name of the plugin.", VSTName, ""),
		vstProp("plugin_path", KString, "The absolute path to the plugin binary.", VSTPluginPath, ""),
		vstProp("state", KBytes, "Plugin specific preset data blob.", VSTState, ""),
		vstProp("vendor", KString, "Plugin developer (vendor) name.", VSTVendor, ""),
		vstProp("io", KBytes, "Raw plugin IO info sub event (PyFLP's PluginIOInfo is an unimplemented stub).", VSTIO, ""),
		vstProp("inputs", KBytes, "Raw input info sub event.", VSTInputs, ""),
		vstProp("outputs", KBytes, "Raw output info sub event.", VSTOutputs, ""),
		vstProp("plugin_info", KBytes, "Raw plugin info sub event.", VSTPluginInfo, ""),
	}
)

// ---------------------------------------------------------------------------
// Native plugins
// ---------------------------------------------------------------------------

func notebookPages(m *Model) (interface{}, bool) {
	c := m.container(IDPluginData)
	if c == nil {
		return nil, false
	}
	pages, ok := c.List("pages")
	if !ok {
		return nil, false
	}
	out := map[int64]string{}
	for _, p := range pages {
		idx, _ := p.Int("index")
		s, _ := p.Str("value")
		out[idx] = s
	}
	return out, true
}

type pluginKind struct {
	Name     string
	Internal string
	Event    *EventType
	Specs    []*PropSpec
}

var pluginKinds = []*pluginKind{
	{"VSTPlugin", "Fruity Wrapper", VSTPluginEvent, vstOwnSpecs},
	{"BooBass", "BooBass", BooBassEvent, []*PropSpec{
		np("bass", KInt, "Volume of the bass region. Min 0, Max 65535, Default 32767.", "bass"),
		np("high", KInt, "Volume of the high region. Min 0, Max 65535, Default 32767.", "high"),
		np("mid", KInt, "Volume of the mid region. Min 0, Max 65535, Default 32767.", "mid"),
	}},
	{"FruitKick", "Fruit Kick", FruitKickEvent, []*PropSpec{
		np("amp_decay", KInt, "Amplitude (volume) decay length. Linear, 0-256, default 128.", "amp_decay"),
		np("click", KInt, "Amount of phase offset added to produce a click. 0-64, default 32.", "click"),
		np("distortion", KInt, "Linear, 0-128. Defaults to minimum.", "distortion"),
		np("freq_decay", KInt, "Pitch sweep time / pitch decay. Linear, 0-256, default 64.", "freq_decay"),
		np("max_freq", KInt, "Start frequency. Linear, -900 to 3600, default 0.", "max_freq"),
		np("min_freq", KInt, "Sweep to / end frequency. Linear, -1200 to 1200, default -600.", "min_freq"),
	}},
	{"FruityBalance", "Fruity Balance", FruityBalanceEvent, []*PropSpec{
		np("pan", KInt, "Linear, -128 (100% left) to 127 (100% right), default 0.", "pan"),
		np("volume", KInt, "Logarithmic, 0 to 320, default 256 (0 dB).", "volume"),
	}},
	{"FruityBloodOverdrive", "Fruity Blood Overdrive", FruityBloodOverdriveEvent, []*PropSpec{
		np("pre_band", KInt, "Linear, 0-10000, default 0.", "pre_band"),
		np("color", KInt, "Linear, 0-10000, default 5000.", "color"),
		np("pre_amp", KInt, "Linear, 0-10000, default 0.", "pre_amp"),
		np("x100", KBool, "Boolean, default off.", "x100"),
		np("post_filter", KInt, "Linear, 0-10000, default 0.", "post_filter"),
		np("post_gain", KInt, "Linear, 0-10000, default 10000.", "post_gain"),
	}},
	{"FruityCenter", "Fruity Center", FruityCenterEvent, []*PropSpec{
		np("enabled", KBool, "Removes DC offset if true; behaves like a bypass button (Status).", "enabled"),
	}},
	{"FruityFastDist", "Fruity Fast Dist", FruityFastDistEvent, []*PropSpec{
		np("kind", KString, "Distortion kind: A or B.", "kind"),
		np("mix", KInt, "Linear, 0-128. Defaults to maximum.", "mix"),
		np("post", KInt, "Linear, 0-128. Defaults to maximum.", "post"),
		np("pre", KInt, "Linear, 64-192, default 128.", "pre"),
		np("threshold", KInt, "Linear, stepped, 1-10. Defaults to maximum.", "threshold"),
	}},
	{"FruityNotebook2", "Fruity NoteBook 2", FruityNotebook2Event, []*PropSpec{
		np("active_page", KInt, "Active page number of the notebook. Min 0, Max 100.", "active_page"),
		np("editable", KBool, "Whether the notebook is marked as editable or read-only.", "editable"),
		readOnly(customProp("pages", KStrMap, "A map of page numbers to their contents.", notebookPages, nil)),
	}},
	{"FruitySend", "Fruity Send", FruitySendEvent, []*PropSpec{
		np("dry", KInt, "Linear, 0-256. Defaults to maximum.", "dry"),
		np("pan", KInt, "Linear, -128 to 127, default 0.", "pan"),
		np("send_to", KInt, "Target insert index; defaults to -1 (Master).", "send_to"),
		np("volume", KInt, "Logarithmic, 0-320, default 256.", "volume"),
	}},
	{"FruitySoftClipper", "Fruity Soft Clipper", FruitySoftClipperEvent, []*PropSpec{
		np("post", KInt, "Linear, 0-160, default 128.", "post"),
		np("threshold", KInt, "Logarithmic, 1-127, default 100.", "threshold"),
	}},
	{"FruityStereoEnhancer", "Fruity Stereo Enhancer", FruityStereoEnhancerEvent, []*PropSpec{
		np("effect_position", KString, "pre or post. Defaults to post.", "effect_position"),
		np("pan", KInt, "Linear, -128 to 127, default 0.", "pan"),
		np("phase_inversion", KString, "none, left or right. Defaults to none.", "phase_inversion"),
		np("phase_offset", KInt, "Linear, -512 to 512, default 0.", "phase_offset"),
		np("stereo_separation", KInt, "Linear, -96 to 96, default 0.", "stereo_separation"),
		np("volume", KInt, "Logarithmic, 0-320, default 256.", "volume"),
	}},
	{"Plucked", "Plucked!", PluckedEvent, []*PropSpec{
		np("color", KInt, "Linear, 0-128, default 64.", "color"),
		np("decay", KInt, "Linear, 0-256, default 128.", "decay"),
		np("gate", KBool, "Stops the voices abruptly when released.", "gate"),
		np("normalize", KBool, "Same decay is tried to be used for all semitones.", "normalize"),
		np("widen", KBool, "Enriches the stereo panorama of the sound.", "widen"),
	}},
	{"Soundgoodizer", "Soundgoodizer", SoundgoodizerEvent, []*PropSpec{
		np("amount", KInt, "Logarithmic, 0-1000, default 600.", "amount"),
		np("mode", KString, "4 preset modes: A, B, C or D. Defaults to A.", "mode"),
	}},
}

func pluginKindByEvent(t *EventType) *pluginKind {
	for _, k := range pluginKinds {
		if k.Event == t {
			return k
		}
	}
	return nil
}

// PluginKinds returns the model type names of all implemented plugins.
func PluginKinds() []string {
	out := make([]string, len(pluginKinds))
	for i, k := range pluginKinds {
		out[i] = k.Name
	}
	return out
}

// Plugin is a native or VST plugin loaded into a channel or an effect slot.
type Plugin struct {
	*Model
	// InternalName is the name FL uses to decide the type of plugin data;
	// empty for plugins which are not implemented.
	InternalName string
}

func (p *Plugin) specsFor(extra []*PropSpec) []*PropSpec {
	return append(basePluginSpecs(), extra...)
}

// IsVST reports whether this is a VST / VST3 plugin.
func (p *Plugin) IsVST() bool { return p.Kind == "VSTPlugin" }

func (p *Plugin) vstSub(kind string, specs []*PropSpec) *Model {
	return newModel(kind, p.Events, specs)
}

// Automation returns the VST automation options (VST plugins only).
func (p *Plugin) Automation() *Model { return p.vstSub("VSTAutomationOptions", vstAutomationSpecs) }

// Compatibility returns the VST compatibility options.
func (p *Plugin) Compatibility() *Model {
	return p.vstSub("VSTCompatibilityOptions", vstCompatibilitySpecs)
}

// MIDI returns the VST MIDI options.
func (p *Plugin) MIDI() *Model { return p.vstSub("VSTMIDIOptions", vstMIDISpecs) }

// Processing returns the VST processing options.
func (p *Plugin) Processing() *Model { return p.vstSub("VSTProcessingOptions", vstProcessingSpecs) }

// UI returns the VST user interface options.
func (p *Plugin) UI() *Model { return p.vstSub("VSTUIOptions", vstUISpecs) }

// pluginOf implements PyFLP's PluginProp.__get__: it returns the plugin held
// by ins when its data event is one of the allowed kinds. Unimplemented
// plugins (UnknownDataEvent) are returned as a generic plugin exposing only
// wrapper properties.
func pluginOf(ins *EventTree, allowed ...string) *Plugin {
	data := ins.FirstOf(IDPluginData)
	if data == nil {
		return nil
	}
	view := ins.Subtree(func(e *Event) Select {
		if e.id == IDPluginWrapper || e.id == IDPluginData {
			return Include
		}
		return Skip
	})
	if data.typ == UnknownDataEvent {
		p := &Plugin{}
		p.Model = newModel("Plugin", view, basePluginSpecs())
		return p
	}
	kind := pluginKindByEvent(data.typ)
	if kind == nil {
		return nil
	}
	ok := false
	for _, a := range allowed {
		if a == kind.Name {
			ok = true
		}
	}
	if !ok {
		return nil
	}
	p := &Plugin{InternalName: kind.Internal}
	p.Model = newModel(kind.Name, view, p.specsFor(kind.Specs))
	return p
}

// setPlugin implements PluginProp.__set__: it replaces the wrapper and data
// events of ins by those of value and updates the internal name.
func setPlugin(ins *EventTree, value *Plugin) error {
	if value == nil {
		return fmt.Errorf("%w: nil plugin", ErrFLP)
	}
	if value.InternalName != "" {
		if e := ins.FirstOf(IDPluginInternalName); e != nil {
			if err := e.SetValue(value.InternalName); err != nil {
				return err
			}
		}
	}
	for _, id := range []EventID{IDPluginData, IDPluginWrapper} {
		src := value.Events.FirstOf(id)
		if src == nil {
			continue
		}
		for _, ie := range ins.lst {
			if ie.E.id == id {
				ie.E = src
			}
		}
	}
	return nil
}
