package pyflp

// Enumerations (ct.EnumBase / enum.IntEnum in PyFLP) and bit flags
// (enum.IntFlag).

// FileFormat is the internal format marker FL Studio uses to tell project
// files and the various preset / state file types apart.
type FileFormat int

const (
	FormatNone           FileFormat = -1   // temporary file
	FormatProject        FileFormat = 0    // project (.flp)
	FormatScore          FileFormat = 0x10 // score (.fsc)
	FormatAutomation     FileFormat = 24   // controller events + automation (FST)
	FormatChannelState   FileFormat = 0x20 // entire channel incl. plugin events (FST)
	FormatPluginState    FileFormat = 0x30 // native plugin on a channel / slot (FST)
	FormatGeneratorState FileFormat = 0x31 // VST instrument (FST)
	FormatFXState        FileFormat = 0x32 // VST effect (FST)
	FormatInsertState    FileFormat = 0x40 // insert and all its slots (FST)
	FormatProbablyPatch  FileFormat = 0x50 // patcher presets are stored as PluginState
)

var FileFormatEnum = NewEnum("FileFormat",
	-1, "None", 0, "Project", 0x10, "Score", 24, "Automation", 0x20, "ChannelState",
	0x30, "PluginState", 0x31, "GeneratorState", 0x32, "FXState", 0x40, "InsertState",
	0x50, "ProbablyPatcher")

// String returns the name of the format.
func (f FileFormat) String() string { return FileFormatEnum.NameOf(int64(f)) }

// Valid reports whether f is a known file format.
func (f FileFormat) Valid() bool { _, ok := FileFormatEnum.Names[int64(f)]; return ok }

// ValidPPQs are the timebases supported by FL Studio.
var ValidPPQs = []int{24, 48, 72, 96, 120, 144, 168, 192, 384, 768, 960}

// MinTempo is the minimum tempo (in BPM) FL Studio supports.
const MinTempo = 10.0

func validPPQ(p int) bool {
	for _, v := range ValidPPQs {
		if v == p {
			return true
		}
	}
	return false
}

var (
	PanLawEnum = NewEnum("PanLaw", 0, "Circular", 2, "Triangular")

	TrackMotionEnum = NewEnum("TrackMotion", 0, "Stay", 1, "OneShot", 2, "MarchWrap",
		3, "MarchStay", 4, "MarchStop", 5, "Random", 6, "ExclusiveRandom")
	TrackPressEnum = NewEnum("TrackPress", 0, "Retrigger", 1, "HoldStop", 2, "HoldMotion", 3, "Latch")
	TrackSyncEnum  = NewEnum("TrackSync", 0, "Off", 1, "QuarterBeat", 2, "HalfBeat", 3, "Beat",
		4, "TwoBeats", 5, "FourBeats", 6, "Auto")

	LFOShapeEnum = NewEnum("LFOShape", 0, "Sine", 1, "Triangle", 2, "Pulse")

	FilterTypeEnum = NewEnum("FilterType", 0, "FastLP", 1, "LP", 2, "BP", 3, "HP", 4, "BS",
		5, "LPx2", 6, "SVFLP", 7, "SVFLPx2")

	ArpDirectionEnum = NewEnum("ArpDirection", 0, "Off", 1, "Up", 2, "Down", 3, "UpDownBounce",
		4, "UpDownSticky", 5, "Random")

	DeclickModeEnum = NewEnum("DeclickMode", 0, "OutOnly", 1, "TransientNoBleeding", 2, "Transient",
		3, "Generic", 4, "Smooth", 5, "Crossfade")

	StretchModeEnum = NewEnum("StretchMode", -1, "Stretch", 0, "Resample", 1, "E3Generic", 2, "E3Mono",
		3, "SliceStretch", 4, "SliceMap", 5, "Auto", 6, "E2Generic", 7, "E2Transient", 8, "E2Mono",
		9, "E2Speech")

	ChannelTypeEnum = NewEnum("ChannelType", 0, "Sampler", 2, "Native", 3, "Layer", 4, "Instrument",
		5, "Automation")

	ReverbTypeEnum = NewEnum("ReverbType", 0, "A", 65536, "B")

	WrapperPageEnum = NewEnum("WrapperPage", 0, "Editor", 1, "Settings", 3, "Sample", 4, "Envelope",
		5, "Miscellaneous")

	TimeMarkerTypeEnum = NewEnum("TimeMarkerType", 0, "Marker", 134217728, "Signature")

	InsertDockEnum = NewEnum("InsertDock", 0, "Left", 1, "Middle", 2, "Right")
)

// ChannelType values (the ChannelID.Type event).
const (
	ChannelTypeSampler    = 0 // inbuilt Sampler
	ChannelTypeNative     = 2 // audio clips and other native synths
	ChannelTypeLayer      = 3
	ChannelTypeInstrument = 4
	ChannelTypeAutomation = 5
)

// Reverb types / track enums / dock as plain constants.
const (
	ReverbTypeA = 0
	ReverbTypeB = 65536

	TimeMarkerTypeMarker    = 0
	TimeMarkerTypeSignature = 134217728

	DockLeft   = 0
	DockMiddle = 1
	DockRight  = 2
)

// Mixer parameter IDs (MixerParamsEvent items).
const (
	MixerSlotEnabled      = 0
	MixerSlotMix          = 1
	MixerRouteVolStart    = 64 // 64 - 191 are send levels
	MixerVolume           = 192
	MixerPan              = 193
	MixerStereoSeparation = 194
	MixerLowGain          = 208
	MixerMidGain          = 209
	MixerHighGain         = 210
	MixerLowFreq          = 216
	MixerMidFreq          = 217
	MixerHighFreq         = 218
	MixerLowQ             = 224
	MixerMidQ             = 225
	MixerHighQ            = 226
)

// Flag masks.
const (
	// Insert flags.
	InsertPolarityReversed          = 1 << 0
	InsertSwapLeftRight             = 1 << 1
	InsertEnableEffects             = 1 << 2
	InsertEnabled                   = 1 << 3
	InsertDisableThreadedProcessing = 1 << 4
	InsertDockMiddle                = 1 << 6
	InsertDockRight                 = 1 << 7
	InsertSeparatorShown            = 1 << 10
	InsertLocked                    = 1 << 11
	InsertSolo                      = 1 << 12
	InsertAudioTrack                = 1 << 15

	// Envelope / LFO flags.
	EnvEnvelopeTempoSync = 1 << 0
	EnvLFOTempoSync      = 1 << 1
	EnvUnknown           = 1 << 2
	EnvLFOPhaseRetrig    = 1 << 5

	DelayPingPong = 1 << 1
	DelayFatMode  = 1 << 2

	PolyMono  = 1 << 0
	PolyPorta = 1 << 1

	FXFadeStereo = 1 << 0
	FXReverse    = 1 << 1
	FXClip       = 1 << 2
	FXSwapStereo = 1 << 8

	LayerRandom    = 1 << 0
	LayerCrossfade = 1 << 1

	SamplerResample         = 1 << 0
	SamplerLoadRegions      = 1 << 1
	SamplerLoadSliceMarkers = 1 << 2
	SamplerUsesLoopPoints   = 1 << 3
	SamplerKeepOnDisk       = 1 << 8

	NoteSlide = 1 << 3

	// Plugin wrapper flags.
	WrapperVisible            = 1 << 0
	WrapperDisabled           = 1 << 1
	WrapperDetached           = 1 << 2
	WrapperGenerator          = 1 << 4
	WrapperSmartDisable       = 1 << 5
	WrapperThreadedProcessing = 1 << 6
	WrapperDemoMode           = 1 << 7
	WrapperHideSettings       = 1 << 8
	WrapperMinimized          = 1 << 9
	WrapperDirectX            = 1 << 16
	WrapperEditorSize         = 2 << 16

	// VST flags (VSTFlags event, "flags").
	VSTSendPBRange         = 1 << 0
	VSTFixedSizeBuffers    = 1 << 1
	VSTNotifyRender        = 1 << 2
	VSTProcessInactive     = 1 << 3
	VSTDontSendRelVelo     = 1 << 5
	VSTDontNotifyChanges   = 1 << 6
	VSTSendLoopPos         = 1 << 11
	VSTAllowThreaded       = 1 << 12
	VSTKeepFocus           = 1 << 15
	VSTDontKeepCPUState    = 1 << 16
	VSTSendModX            = 1 << 17
	VSTLoadBridged         = 1 << 18
	VSTExternalWindow      = 1 << 21
	VSTUpdateWhenHidden    = 1 << 23
	VSTDontResetOnTransport = 1 << 25
	VSTDPIAwareBridged     = 1 << 26
	VSTAcceptFileDrop      = 1 << 28
	VSTAllowSmartDisable   = 1 << 29
	VSTScaleEditor         = 1 << 30
	VSTDontUseTimeOffset   = 1 << 31

	// VST flags2.
	VST2ProcessMaxSize = 1 << 0
	VST2UseMaxFromHost = 1 << 1
)
