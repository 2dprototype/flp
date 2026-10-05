package pyflp

import (
	"fmt"
)

// ---------------------------------------------------------------------------
// Property tables of the nested models
// ---------------------------------------------------------------------------

func parameters(name string, kind PropKind, doc, key string) *PropSpec {
	return stProp(name, kind, doc, IDChannelParameters, key)
}

var arpSpecs = []*PropSpec{
	parameters("chord", KInt, "Index of the selected arpeggio chord.", "arp.chord"),
	withEnum(parameters("direction", KEnum, "Arpeggio direction.", "arp.direction"), ArpDirectionEnum),
	parameters("gate", KFloat, "Delay between two successive notes played.", "arp.gate"),
	parameters("range", KInt, "Range (in octaves).", "arp.range"),
	parameters("repeat", KInt, "Number of times a note is repeated. New in FL Studio v4.5.2.", "arp.repeat"),
	parameters("slide", KBool, "Whether arpeggio will slide between notes.", "arp.slide"),
	parameters("time", KFloat, "Delay between two successive notes played.", "arp.time"),
}

func delayModXY(index int) *PropSpec {
	name := "mod_x"
	if index == 1 {
		name = "mod_y"
	}
	return customProp(name, KInt, "Min = 0. Max = 256. Default = 128.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDChannelDelayModXY)
			if e == nil {
				return nil, false
			}
			p, ok := e.Value().([2]int64)
			if !ok {
				return nil, false
			}
			return p[index], true
		},
		func(m *Model, v interface{}) error {
			e := m.event(IDChannelDelayModXY)
			if e == nil {
				return cannotSet(IDChannelDelayModXY)
			}
			p, _ := e.Value().([2]int64)
			p[index], _ = toInt64(v)
			return e.SetValue(p)
		})
}

var delaySpecs = []*PropSpec{
	stProp("echoes", KInt, "Number of echoes generated for each note. Min = 1. Max = 10.", IDChannelDelay, "echoes"),
	flagProp("fat_mode", "Fat mode. New in FL Studio v3.4.0.", IDChannelParameters, "delay.flags", DelayFatMode, false),
	stProp("feedback", KInt, "Factor with which the volume of every next echo is multiplied. 0-25600.", IDChannelDelay, "feedback"),
	delayModXY(0),
	delayModXY(1),
	stProp("pan", KInt, "-6400 (100% left) to 6400 (100% right).", IDChannelDelay, "pan"),
	flagProp("ping_pong", "Ping pong. New in FL Studio v1.7.6.", IDChannelParameters, "delay.flags", DelayPingPong, false),
	stProp("pitch_shift", KInt, "Pitch shift (in cents). -1200 to 1200.", IDChannelDelay, "pitch_shift"),
	stProp("time", KInt, "Tempo-synced delay time. PPQ dependant.", IDChannelDelay, "time"),
}

var filterSpecs = []*PropSpec{
	stProp("mod_x", KInt, "Filter cutoff. Min = 0. Max = 256. Defaults to maximum.", IDChannelLevels, "filter.mod_x"),
	stProp("mod_y", KInt, "Filter resonance. Min = 0. Max = 256. Defaults to minimum.", IDChannelLevels, "filter.mod_y"),
	withEnum(stProp("type", KEnum, "Filter type. Defaults to FastLP.", IDChannelLevels, "filter.type"), FilterTypeEnum),
}

var levelAdjustsSpecs = []*PropSpec{
	stProp("mod_x", KInt, "Mod X level adjust.", IDChannelLevelAdjusts, "mod_x"),
	stProp("mod_y", KInt, "Mod Y level adjust.", IDChannelLevelAdjusts, "mod_y"),
	stProp("pan", KInt, "Pan level adjust.", IDChannelLevelAdjusts, "pan"),
	stProp("volume", KInt, "Volume level adjust.", IDChannelLevelAdjusts, "volume"),
}

var timeSpecs = []*PropSpec{
	evProp("swing", KInt, "Percentage of the rack swing that affects this channel. 0-128.", IDChannelSwing),
	parameters("gate", KInt, "Logarithmic. Min 450, Max 1446, Disabled 1447.", "time.gate"),
	evProp("shift", KInt, "Fine time shift. Nonlinear. 0-1024.", IDChannelTimeShift),
	parameters("full_porta", KBool, "Whether gate is bypassed when portamento is on.", "time.full_porta"),
}

func reverbType(m *Model) (int64, bool) {
	e := m.event(IDChannelReverb)
	if e == nil {
		return 0, false
	}
	v, _ := e.Int()
	if v >= ReverbTypeB {
		return ReverbTypeB, true
	}
	return ReverbTypeA, true
}

var reverbSpecs = []*PropSpec{
	withEnum(customProp("type", KEnum, "Reverb type A or B.", func(m *Model) (interface{}, bool) {
		t, ok := reverbType(m)
		return t, ok
	}, func(m *Model, v interface{}) error {
		e := m.event(IDChannelReverb)
		if e == nil {
			return cannotSet(IDChannelReverb)
		}
		cur, _ := e.Int()
		mix := cur
		if cur >= ReverbTypeB {
			mix = cur - ReverbTypeB
		}
		t, _ := toInt64(v)
		return e.SetValue(t + mix)
	}), ReverbTypeEnum),
	customProp("mix", KInt, "Mix % (wet). Min 0, Max 256.", func(m *Model) (interface{}, bool) {
		e := m.event(IDChannelReverb)
		if e == nil {
			return nil, false
		}
		cur, _ := e.Int()
		t, _ := reverbType(m)
		return cur - t, true
	}, func(m *Model, v interface{}) error {
		e := m.event(IDChannelReverb)
		if e == nil {
			return cannotSet(IDChannelReverb)
		}
		t, _ := reverbType(m)
		mix, _ := toInt64(v)
		return e.SetValue(t + mix)
	}),
}

var fxSpecs = []*PropSpec{
	evProp("boost", KInt, "Pre-amp gain. 0-256. New in FL Studio v1.2.12.", IDChannelPreamp),
	flagProp("clip", "Whether output is clipped at 0dB for boost.", IDChannelFXFlags, "", FXClip, false),
	parameters("crossfade", KInt, "Linear, 0-256.", "fx.crossfade"),
	evProp("cutoff", KInt, "Filter Mod X. Min = 16. Max = 1024.", IDChannelCutoff),
	evProp("fade_in", KInt, "Quick fade-in. Min = 0. Max = 1024.", IDChannelFadeIn),
	evProp("fade_out", KInt, "Quick fade-out. Min = 0. Max = 1024. New in FL Studio v1.7.6.", IDChannelFadeOut),
	flagProp("fade_stereo", "Fade stereo.", IDChannelFXFlags, "", FXFadeStereo, false),
	parameters("fix_trim", KBool, "Trim --> Fix legacy precomputed length.", "fx.fix_trim"),
	evProp("freq_tilt", KInt, "Shifts the frequency balance. Bipolar. 0-256, default 128.", IDChannelFreqTilt),
	parameters("inverted", KBool, "Named Reverse polarity in FL's interface.", "fx.inverted"),
	parameters("length", KFloat, "Min = 0.0, Max = 1.0 (SMP START in FL's interface).", "fx.length"),
	parameters("normalize", KBool, "Maximizes volume without clipping by normalizing peaks to 0dB.", "fx.normalize"),
	evProp("pogo", KInt, "Pitch bend effect. Bipolar. 0-512, default 256.", IDChannelPogo),
	parameters("remove_dc", KBool, "Whether DC offset (if present) is removed. New in FL Studio v2.5.0.", "fx.remove_dc"),
	evProp("resonance", KInt, "Filter Mod Y. Min = 0. Max = 640.", IDChannelResonance),
	flagProp("reverse", "Whether sample is reversed or not.", IDChannelFXFlags, "", FXReverse, false),
	evProp("ringmod", KPair, "Ring modulation (mix, frequency); both 0-256, default 128.", IDChannelRingMod),
	parameters("start", KFloat, "Min = 0.0, Max = 1.0. Always 0.0 unless a sample is loaded.", "fx.start"),
	evProp("stereo_delay", KInt, "Linear. Bipolar. 0-4096, default 2048. New in FL Studio v1.3.56.", IDChannelStereoDelay),
	flagProp("swap_stereo", "Whether left and right channels are swapped.", IDChannelFXFlags, "", FXSwapStereo, false),
	parameters("trim", KInt, "Silence trimming threshold. Linear, 0-256.", "fx.trim"),
}

func envKey(name, kind, doc string, key string) *PropSpec {
	return stProp(name, KInt, doc, IDChannelEnvelopeLFO, key)
}

var envelopeSpecs = []*PropSpec{
	stProp("enabled", KBool, "Whether envelope section is enabled.", IDChannelEnvelopeLFO, "envelope.enabled"),
	envKey("predelay", "", "Linear. Min 100, Max 65536.", "envelope.predelay"),
	envKey("amount", "", "Linear. Bipolar. -128 to 128, default 0.", "envelope.amount"),
	envKey("attack", "", "Linear. Min 100, Max 65536, default 20000.", "envelope.attack"),
	envKey("hold", "", "Linear. Min 100, Max 65536, default 20000.", "envelope.hold"),
	envKey("decay", "", "Linear. Min 100, Max 65536, default 30000.", "envelope.decay"),
	envKey("sustain", "", "Linear. Min 0, Max 128, default 50.", "envelope.sustain"),
	envKey("release", "", "Linear. Min 100, Max 65536, default 20000.", "envelope.release"),
	flagProp("synced", "Whether envelope is synced to tempo.", IDChannelEnvelopeLFO, "flags", EnvEnvelopeTempoSync, false),
	envKey("attack_tension", "", "Linear. Bipolar. -128 to 128, default 0. New in FL Studio v3.5.4.", "envelope.attack_tension"),
	envKey("decay_tension", "", "Linear. Bipolar. -128 to 128, default 0. New in FL Studio v3.5.4.", "envelope.decay_tension"),
	envKey("release_tension", "", "Linear. Bipolar. -128 to 128, default -101. New in FL Studio v3.5.4.", "envelope.release_tension"),
}

var samplerLFOSpecs = []*PropSpec{
	envKey("amount", "", "Linear. Bipolar. -128 to 128, default 0.", "lfo.amount"),
	envKey("attack", "", "Linear. Min 100, Max 65536, default 20000.", "lfo.attack"),
	envKey("predelay", "", "Linear. Min 100, Max 65536.", "lfo.predelay"),
	envKey("speed", "", "Logarithmic. Min 200, Max 65536, default 32950.", "lfo.speed"),
	flagProp("synced", "Whether LFO is synced with tempo.", IDChannelEnvelopeLFO, "flags", EnvLFOTempoSync, false),
	flagProp("retrig", "Whether LFO phase is in global / retriggered mode.", IDChannelEnvelopeLFO, "flags", EnvLFOPhaseRetrig, false),
	withEnum(stProp("shape", KEnum, "Sine, triangle or pulse. Default: Sine.", IDChannelEnvelopeLFO, "lfo.shape"), LFOShapeEnum),
}

var polyphonySpecs = []*PropSpec{
	flagProp("mono", "Whether monophonic mode is enabled.", IDChannelPolyphony, "flags", PolyMono, false),
	flagProp("porta", "Portamento. New in FL Studio v3.3.0.", IDChannelPolyphony, "flags", PolyPorta, false),
	stProp("max", KInt, "Max number of voices.", IDChannelPolyphony, "max"),
	stProp("slide", KInt, "Portamento time. Nonlinear. 0-1660, default 820.", IDChannelPolyphony, "slide"),
}

var trackingSpecs = []*PropSpec{
	stProp("middle_value", KInt, "Note index. Min: C0 (0), Max: B10 (131).", IDChannelTracking, "middle_value"),
	stProp("mod_x", KInt, "Bipolar. -256 to 256, default 0.", IDChannelTracking, "mod_x"),
	stProp("mod_y", KInt, "Bipolar. -256 to 256, default 0.", IDChannelTracking, "mod_y"),
	stProp("pan", KInt, "Linear. Bipolar. -256 to 256, default 0.", IDChannelTracking, "pan"),
}

var keyboardSpecs = []*PropSpec{
	evProp("fine_tune", KInt, "-100 to +100 cents.", IDChannelFineTune),
	evPropDefault("root_note", KInt, "Min - 0 (C0), Max - 131 (B10). Defaults to 60.", int64(60), IDChannelRootNote),
	parameters("main_pitch", KBool, "Whether triggered note is affected by changes to the project's main pitch.", "keyboard.main_pitch"),
	parameters("add_root", KBool, "Whether to add root note (instead of pitch) to triggered note (Add to key).", "keyboard.add_root"),
	parameters("key_region", KPair, "A (start_note, end_note) pair representing the playable range.", "keyboard.key_region"),
}

var playbackSpecs = []*PropSpec{
	evProp("ping_pong_loop", KBool, "Ping pong loop.", IDChannelPingPongLoop),
	parameters("start_offset", KInt, "Linear. Min 0, Max 1072693248.", "playback.start_offset"),
	flagProp("use_loop_points", "Whether the sample uses loop points.", IDChannelSamplerFlags, "", SamplerUsesLoopPoints, false),
}

var stretchingSpecs = []*PropSpec{
	withEnum(parameters("mode", KEnum, "Time stretching mode.", "stretching.mode"), StretchModeEnum),
	parameters("multiplier", KFloat, "Logarithmic. Min 0.25, Max 4.0, Default 1.0 (100%).", "stretching.multiplier"),
	parameters("pitch", KInt, "Pitch shift (in cents). Min = -1200. Max = 1200. Defaults to 0.", "stretching.pitch"),
	parameters("time", KMusical, "A (bars, beats, ticks) time.", "stretching.time"),
}

var contentSpecs = []*PropSpec{
	withEnum(parameters("declick_mode", KEnum, "Defaults to OutOnly. New in FL Studio v9.0.0.", "content.declick_mode"), DeclickModeEnum),
	flagProp("keep_on_disk", "Whether a sample is streamed from disk or kept in RAM.", IDChannelSamplerFlags, "", SamplerKeepOnDisk, false),
	flagProp("load_regions", "Load regions found in the sample, if any.", IDChannelSamplerFlags, "", SamplerLoadRegions, false),
	flagProp("load_slices", "Load slice markers.", IDChannelSamplerFlags, "", SamplerLoadSliceMarkers, false),
	flagProp("resample", "Resample.", IDChannelSamplerFlags, "", SamplerResample, false),
}

var automationLFOSpecs = []*PropSpec{
	stProp("amount", KInt, "Linear. Bipolar. -128 to 128, default 64 or 0.", IDChannelAutomation, "lfo.amount"),
}

var automationPointSpecs = []*PropSpec{
	readOnly(itProp("position", KFloat, "PPQ dependant position on the X axis. Cannot be set as of yet.", "position")),
	itProp("tension", KFloat, "A value in the range of 0 to 1.0.", "tension"),
	itProp("value", KFloat, "Position on the Y axis in the range of 0 to 1.0.", "value"),
}

var displayGroupSpecs = []*PropSpec{
	evProp("name", KString, "Name of the display group.", IDDisplayGroupName),
}

// ---------------------------------------------------------------------------
// Channels
// ---------------------------------------------------------------------------

// ChannelClass is the kind of a channel.
type ChannelClass int

const (
	ClassChannel ChannelClass = iota
	ClassAutomation
	ClassLayer
	ClassSampler
	ClassInstrument
)

func (c ChannelClass) String() string {
	return [...]string{"Channel", "Automation", "Layer", "Sampler", "Instrument"}[c]
}

// levelProp is pan / volume: stored in the Levels event if present, else in
// the older word / byte events.
func levelProp(name, key, doc string, wordID, byteID EventID) *PropSpec {
	get := func(m *Model) (interface{}, bool) {
		if m.Events.Contains(IDChannelLevels) {
			return m.fieldValue(IDChannelLevels, key)
		}
		for _, id := range []EventID{wordID, byteID} {
			if e := m.event(id); e != nil {
				return e.Value(), true
			}
		}
		return nil, false
	}
	return customProp(name, KInt, doc, get, func(m *Model, v interface{}) error {
		if _, ok := get(m); !ok {
			return cannotSet()
		}
		if m.Events.Contains(IDChannelLevels) {
			return m.setFieldValue(IDChannelLevels, key, v)
		}
		for _, id := range []EventID{wordID, byteID} {
			if e := m.event(id); e != nil {
				return e.SetValue(v)
			}
		}
		return cannotSet()
	})
}

func channelBaseSpecs() []*PropSpec {
	return []*PropSpec{
		evProp("color", KColor, "Defaults to #5C656A (granite gray). Values below 20 for any component are ignored by FL.", IDPluginColor),
		evProp("internal_name", KString, "Internal name of the channel: empty for stock plugins, 'Fruity Wrapper' for VST instruments.", IDPluginInternalName),
		evProp("enabled", KBool, "Whether the channel is enabled.", IDChannelIsEnabled),
		evProp("icon", KInt, "Internal ID of the icon shown beside the display name.", IDPluginIcon),
		evProp("iid", KInt, "Internal index of the channel.", IDChannelNew),
		evProp("locked", KBool, "Whether in a locked state; mute / solo acts differently when true.", IDChannelIsLocked),
		evProp("name", KString, "The name associated with a channel.", IDPluginName, IDChannelName),
		levelProp("pan", "pan", "Linear. Bipolar. Min 0, Max 12800, default 6400.", IDChannelPanWord, IDChannelPanByte),
		levelProp("volume", "volume", "Nonlinear. Min 0, Max 12800, default 10000.", IDChannelVolWord, IDChannelVolByte),
		readOnly(customProp("zipped", KBool, "Whether the channel is zipped / minimized.", func(m *Model) (interface{}, bool) {
			if e := m.event(IDChannelZipped); e != nil {
				return e.Value(), true
			}
			return false, true
		}, nil)),
		readOnly(customProp("display_name", KString, "The name of the channel that will be displayed in FL Studio.", func(m *Model) (interface{}, bool) {
			if n, ok := m.Str("name"); ok && n != "" {
				return n, true
			}
			n, ok := m.Str("internal_name")
			return n, ok
		}, nil)),
	}
}

func samplerInstrumentSpecs() []*PropSpec {
	return []*PropSpec{
		evProp("cut_group", KPair, "Cut group (cut self, cut by). To cut itself when retriggered, set the same value for both.", IDChannelCutGroup),
		evProp("insert", KInt, "Index of the insert the channel is routed to. Current = -1, Master = 0, ...", IDChannelRoutedTo),
		stProp("pitch_shift", KInt, "-4800 to +4800 (cents).", IDChannelLevels, "pitch_shift"),
	}
}

func samplerSpecs() []*PropSpec {
	return append(samplerInstrumentSpecs(),
		evProp("au_sample_rate", KInt, "AU-format sample specific.", IDChannelAUSampleRate),
		customProp("sample_path", KString, "Absolute path of the sample on disk; contains %FLStudioFactoryData% for stock samples.",
			func(m *Model) (interface{}, bool) {
				e := m.event(IDChannelSamplePath)
				if e == nil {
					return nil, false
				}
				return e.Value(), true
			},
			func(m *Model, v interface{}) error {
				e := m.event(IDChannelSamplePath)
				if e == nil {
					return cannotSet(IDChannelSamplePath)
				}
				s, _ := v.(string)
				if s == "." {
					s = ""
				}
				return e.SetValue(s)
			}))
}

func layerSpecs() []*PropSpec {
	return []*PropSpec{
		flagProp("crossfade", "Layering: crossfade.", IDChannelLayerFlags, "", LayerCrossfade, false),
		flagProp("random", "Layering: random.", IDChannelLayerFlags, "", LayerRandom, false),
	}
}

// Channel is a channel in the channel rack. Its Class decides which
// properties and navigation methods are meaningful.
type Channel struct {
	*Model
	Class    ChannelClass
	channels map[int64]*Channel
	group    *Model
}

// NamedModel is a model with a label, used for envelopes, LFOs and tracking.
type NamedModel struct {
	Name string
	*Model
}

func newChannel(class ChannelClass, et *EventTree, channels map[int64]*Channel, group *Model) *Channel {
	specs := channelBaseSpecs()
	switch class {
	case ClassLayer:
		specs = append(specs, layerSpecs()...)
	case ClassSampler:
		specs = append(specs, samplerSpecs()...)
	case ClassInstrument:
		specs = append(specs, samplerInstrumentSpecs()...)
	}
	return &Channel{Model: newModel(class.String(), et, specs), Class: class, channels: channels, group: group}
}

// IID returns the channel's internal index.
func (c *Channel) IID() int64 { v, _ := c.Int("iid"); return v }

// DisplayName returns the name FL displays.
func (c *Channel) DisplayName() string { s, _ := c.Str("display_name"); return s }

func (c *Channel) String() string {
	name, _ := c.Str("display_name")
	s := fmt.Sprintf("%s (name=%q, iid=%d", c.Class, name, c.IID())
	switch c.Class {
	case ClassLayer:
		s += fmt.Sprintf(", %d children", c.Len())
	case ClassSampler:
		p, _ := c.Str("sample_path")
		s += fmt.Sprintf(", sample_path=%q", p)
	}
	return s + ")"
}

// Group is the display group / filter under which the channel is grouped.
func (c *Channel) Group() *Model { return c.group }

func (c *Channel) sub(kind string, specs []*PropSpec, ids ...EventID) *Model {
	return newModel(kind, c.Events.SubtreeIDs(ids...), specs)
}

// Keyboard: Miscellaneous functions (page) --> Keyboard (Sampler and Instrument).
func (c *Channel) Keyboard() *Model {
	return c.sub("Keyboard", keyboardSpecs, IDChannelFineTune, IDChannelRootNote, IDChannelParameters)
}

// Arp: Miscellaneous functions --> Arpeggiator.
func (c *Channel) Arp() *Model { return c.sub("Arp", arpSpecs, IDChannelParameters) }

// Delay: Echo delay / fat mode.
func (c *Channel) Delay() *Model {
	return c.sub("Delay", delaySpecs, IDChannelDelay, IDChannelDelayModXY, IDChannelParameters)
}

// LevelAdjusts: Miscellaneous functions --> Level adjustments.
func (c *Channel) LevelAdjusts() *Model {
	return c.sub("LevelAdjusts", levelAdjustsSpecs, IDChannelLevelAdjusts)
}

// Polyphony: Miscellaneous functions --> Polyphony.
func (c *Channel) Polyphony() *Model { return c.sub("Polyphony", polyphonySpecs, IDChannelPolyphony) }

// Time: Miscellaneous functions --> Time.
func (c *Channel) Time() *Model {
	return c.sub("Time", timeSpecs, IDChannelSwing, IDChannelTimeShift, IDChannelParameters)
}

// Tracking returns the Volume and Keyboard tracking models (nil if none).
func (c *Channel) Tracking() []NamedModel {
	if !c.Events.Contains(IDChannelTracking) {
		return nil
	}
	names := []string{"volume", "keyboard"}
	var out []NamedModel
	for i, et := range c.Events.Separate(IDChannelTracking) {
		if i >= len(names) {
			break
		}
		out = append(out, NamedModel{names[i], newModel("Tracking", et, trackingSpecs)})
	}
	return out
}

// Plugin returns the plugin loaded in an Instrument channel (nil if none /
// not an instrument).
func (c *Channel) Plugin() *Plugin {
	if c.Class != ClassInstrument {
		return nil
	}
	return pluginOf(c.Events, "VSTPlugin", "BooBass", "FruitKick", "Plucked")
}

// SetPlugin replaces the plugin of an instrument channel.
func (c *Channel) SetPlugin(p *Plugin) error {
	if c.Class != ClassInstrument {
		return fmt.Errorf("%w: only instrument channels hold plugins", ErrFLP)
	}
	return setPlugin(c.Events, p)
}

var envelopeNames = []string{"Panning", "Volume", "Mod X", "Mod Y", "Pitch"}

// Envelopes returns an Envelope each for Panning, Volume, Mod X, Mod Y and
// Pitch (Sampler only).
func (c *Channel) Envelopes() []NamedModel {
	return c.envLFO("Envelope", envelopeSpecs)
}

// LFOs returns an LFO each for Panning, Volume, Mod X, Mod Y and Pitch
// (Sampler only).
func (c *Channel) LFOs() []NamedModel { return c.envLFO("SamplerLFO", samplerLFOSpecs) }

func (c *Channel) envLFO(kind string, specs []*PropSpec) []NamedModel {
	if c.Class != ClassSampler || !c.Events.Contains(IDChannelEnvelopeLFO) {
		return nil
	}
	var out []NamedModel
	for i, et := range c.Events.Separate(IDChannelEnvelopeLFO) {
		if i >= len(envelopeNames) {
			break
		}
		out = append(out, NamedModel{envelopeNames[i], newModel(kind, et, specs)})
	}
	return out
}

// Content: Sample settings --> Content (Sampler only).
func (c *Channel) Content() *Model {
	return c.sub("Content", contentSpecs, IDChannelSamplerFlags, IDChannelParameters)
}

// Filter (Sampler only).
func (c *Channel) Filter() *Model { return c.sub("Filter", filterSpecs, IDChannelLevels) }

// FX: Sample settings --> Precomputed effects (Sampler only).
func (c *Channel) FX() *Model {
	return c.sub("FX", fxSpecs, IDChannelCutoff, IDChannelFadeIn, IDChannelFadeOut, IDChannelFreqTilt,
		IDChannelParameters, IDChannelPogo, IDChannelPreamp, IDChannelResonance, IDChannelReverb,
		IDChannelRingMod, IDChannelStereoDelay, IDChannelFXFlags)
}

// Reverb is the precalculated reverb of the FX section.
func (c *Channel) Reverb() *Model { return c.sub("Reverb", reverbSpecs, IDChannelReverb) }

// Playback: Sample settings --> Playback (Sampler only).
func (c *Channel) Playback() *Model {
	return c.sub("Playback", playbackSpecs, IDChannelSamplerFlags, IDChannelPingPongLoop, IDChannelParameters)
}

// Stretching: Sample settings --> Time stretching (Sampler only).
func (c *Channel) Stretching() *Model {
	return c.sub("TimeStretching", stretchingSpecs, IDChannelParameters)
}

// LFO is the automation clip LFO (Automation only).
func (c *Channel) LFO() *Model { return c.sub("AutomationLFO", automationLFOSpecs, IDChannelAutomation) }

// Points returns the automation points of an automation clip.
func (c *Channel) Points() []*Model {
	if c.Class != ClassAutomation {
		return nil
	}
	e := c.Events.FirstOf(IDChannelAutomation)
	if e == nil {
		return nil
	}
	cont, ok := e.Container()
	if !ok {
		return nil
	}
	pts, _ := cont.List("points")
	out := make([]*Model, len(pts))
	for i, p := range pts {
		out[i] = newItemModel("AutomationPoint", p, i, e, automationPointSpecs)
	}
	return out
}

// Children returns the channels a Layer is made of.
func (c *Channel) Children() ([]*Channel, error) {
	if c.Class != ClassLayer {
		return nil, nil
	}
	var out []*Channel
	for _, e := range c.Events.Get(IDChannelChildren) {
		v, _ := e.Int()
		ch, ok := c.channels[v]
		if !ok {
			return out, channelNotFound(v)
		}
		out = append(out, ch)
	}
	return out, nil
}

// Len returns the number of children of a layer (0 for other classes).
func (c *Channel) Len() int {
	if c.Class != ClassLayer {
		return 0
	}
	return c.Events.Count(IDChannelChildren)
}

// ---------------------------------------------------------------------------
// Channel rack
// ---------------------------------------------------------------------------

var rackSpecs = []*PropSpec{
	evProp("fit_to_steps", KInt, "Unknown purpose.", IDRackFitToSteps),
	evProp("height", KInt, "Window height of the channel rack in the interface (in pixels).", IDRackWindowHeight),
	evProp("swing", KInt, "Global channel swing mix. Linear. 0-128, default 0.", IDRackSwing),
}

var channelDivideIDs []EventID

func init() {
	for _, id := range AllIDs() {
		if InGroups(id, GroupChannel, GroupPlugin) {
			channelDivideIDs = append(channelDivideIDs, id)
		}
	}
}

// ChannelRack contains all the channels of a project.
type ChannelRack struct{ *Model }

// Groups returns the display groups (filters) of the rack.
func (r *ChannelRack) Groups() []*Model {
	var out []*Model
	for _, et := range r.Events.Separate(IDDisplayGroupName) {
		out = append(out, newModel("DisplayGroup", et, displayGroupSpecs))
	}
	return out
}

// All returns every channel found in the project.
func (r *ChannelRack) All() ([]*Channel, error) {
	if !r.Events.Contains(IDChannelNew) {
		return nil, ErrNoModelsFound
	}
	groups := r.Groups()
	chmap := map[int64]*Channel{}
	var out []*Channel
	for _, et := range r.Events.Divide(IDChannelNew, channelDivideIDs...) {
		newEv, err := et.First(IDChannelNew)
		if err != nil {
			return out, err
		}
		typEv, err := et.First(IDChannelType)
		if err != nil {
			return out, err
		}
		iid, _ := newEv.Int()
		typ, _ := typEv.Int()

		var group *Model
		if gnEv, err := et.First(IDChannelGroupNum); err == nil {
			if gn, _ := gnEv.Int(); gn >= 0 && int(gn) < len(groups) {
				group = groups[gn]
			}
		}

		class := ClassChannel
		switch typ {
		case ChannelTypeAutomation:
			class = ClassAutomation
		case ChannelTypeLayer:
			class = ClassLayer
		case ChannelTypeSampler:
			class = ClassSampler
		case ChannelTypeInstrument, ChannelTypeNative:
			class = ClassInstrument
		}

		// Audio clips are stored as Instrument until a sample is loaded in them.
		if et.Contains(IDChannelSamplePath) && et.Contains(IDPluginInternalName) {
			if n, _ := et.FirstOf(IDPluginInternalName).Str(); n == "" && class == ClassInstrument {
				class = ClassSampler
			}
		}

		ch := newChannel(class, et, chmap, group)
		chmap[iid] = ch
		out = append(out, ch)
	}
	return out, nil
}

func (r *ChannelRack) filter(class ChannelClass) ([]*Channel, error) {
	all, err := r.All()
	var out []*Channel
	for _, c := range all {
		if c.Class == class {
			out = append(out, c)
		}
	}
	return out, err
}

// Automations returns the automation clips in the project.
func (r *ChannelRack) Automations() ([]*Channel, error) { return r.filter(ClassAutomation) }

// Instruments returns native and 3rd-party synth channels.
func (r *ChannelRack) Instruments() ([]*Channel, error) { return r.filter(ClassInstrument) }

// Layers returns the layer channels.
func (r *ChannelRack) Layers() ([]*Channel, error) { return r.filter(ClassLayer) }

// Samplers returns samplers and audio clips.
func (r *ChannelRack) Samplers() ([]*Channel, error) { return r.filter(ClassSampler) }

// Len returns the number of channels; ErrNoModelsFound if there are none.
func (r *ChannelRack) Len() (int, error) {
	if !r.Events.Contains(IDChannelNew) {
		return 0, ErrNoModelsFound
	}
	return r.Events.Count(IDChannelNew), nil
}

// ByIID returns the channel with the internal index.
func (r *ChannelRack) ByIID(iid int64) (*Channel, error) {
	all, err := r.All()
	for _, c := range all {
		if c.IID() == iid {
			return c, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, channelNotFound(iid)
}

// ByName returns the channel with the display name.
func (r *ChannelRack) ByName(name string) (*Channel, error) {
	all, err := r.All()
	for _, c := range all {
		if c.DisplayName() == name {
			return c, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, channelNotFound(name)
}

func (r *ChannelRack) String() string {
	n, _ := r.Len()
	return fmt.Sprintf("ChannelRack - %d channels", n)
}
