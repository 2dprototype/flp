package flpdiff
// ───────────── JSON-facing summary types (field names mirror the TS ProjectSummary) ─────────────

type rgbaOut struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
	A int `json:"a"`
}

type levelsOut struct {
	Pan        int32  `json:"pan"`
	Volume     uint32 `json:"volume"`
	PitchShift int32  `json:"pitch_shift"`
	FilterModX uint32 `json:"filter_mod_x"`
	FilterModY uint32 `json:"filter_mod_y"`
	FilterType uint32 `json:"filter_type"`
}

type insertFlagsOut struct {
	PolarityReversed          bool `json:"polarityReversed"`
	SwapLeftRight             bool `json:"swapLeftRight"`
	EnableEffects             bool `json:"enableEffects"`
	Enabled                   bool `json:"enabled"`
	DisableThreadedProcessing bool `json:"disableThreadedProcessing"`
	DockMiddle                bool `json:"dockMiddle"`
	DockRight                 bool `json:"dockRight"`
	SeparatorShown            bool `json:"separatorShown"`
	Locked                    bool `json:"locked"`
	Solo                      bool `json:"solo"`
	AudioTrack                bool `json:"audioTrack"`
}

type pluginSummaryOut struct {
	InternalName string  `json:"internalName"`
	Name         *string `json:"name,omitempty"`
	Vendor       *string `json:"vendor,omitempty"`
}

// ChannelSummary is the bridge/list_channels row.
type ChannelSummary struct {
	Iid          int               `json:"iid"`
	Kind         string            `json:"kind"`
	Name         *string           `json:"name,omitempty"`
	SamplePath   *string           `json:"sample_path,omitempty"`
	Plugin       *pluginSummaryOut `json:"plugin,omitempty"`
	Color        *rgbaOut          `json:"color,omitempty"`
	Levels       *levelsOut        `json:"levels,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	PingPongLoop *bool             `json:"pingPongLoop,omitempty"`
	Locked       *bool             `json:"locked,omitempty"`
	Zipped       *bool             `json:"zipped,omitempty"`
}

type slotPluginOut struct {
	Name   string  `json:"name"`
	Vendor *string `json:"vendor"`
}

// SlotSummary is one mixer slot row.
type SlotSummary struct {
	Index      int            `json:"index"`
	PluginName *string        `json:"pluginName,omitempty"`
	HasPlugin  *bool          `json:"hasPlugin,omitempty"`
	Plugin     *slotPluginOut `json:"plugin,omitempty"`
}

// InsertSummary is one mixer insert row.
type InsertSummary struct {
	Index  int             `json:"index"`
	Name   *string         `json:"name,omitempty"`
	Color  *rgbaOut        `json:"color,omitempty"`
	Icon   *int            `json:"icon,omitempty"`
	Output *uint32         `json:"output,omitempty"`
	Input  *uint32         `json:"input,omitempty"`
	Flags  *insertFlagsOut `json:"flags,omitempty"`
	Slots  []SlotSummary   `json:"slots"`
}

type controllerOut struct {
	Position uint32  `json:"position"`
	Channel  int     `json:"channel"`
	Flags    int     `json:"flags"`
	Value    float64 `json:"value"`
}

// PatternSummary is one pattern row.
type PatternSummary struct {
	ID          int             `json:"id"`
	Name        *string         `json:"name,omitempty"`
	Length      *uint32         `json:"length,omitempty"`
	Color       *rgbaOut        `json:"color,omitempty"`
	Looped      *bool           `json:"looped,omitempty"`
	Notes       []Note          `json:"notes"`
	Controllers []controllerOut `json:"controllers"`
}

type clipOut struct {
	Position    uint32  `json:"position"`
	ItemIndex   int     `json:"item_index"`
	Length      uint32  `json:"length"`
	TrackRvidx  int     `json:"track_rvidx"`
	Group       int     `json:"group"`
	ItemFlags   int     `json:"item_flags"`
	StartOffset float64 `json:"start_offset"`
	EndOffset   float64 `json:"end_offset"`
}

type timeMarkerOut struct {
	Kind        string  `json:"kind"`
	Position    uint32  `json:"position"`
	Name        *string `json:"name,omitempty"`
	Numerator   *int    `json:"numerator,omitempty"`
	Denominator *int    `json:"denominator,omitempty"`
}

// ArrangementSummary is one arrangement row.
type ArrangementSummary struct {
	ID          int             `json:"id"`
	Name        *string         `json:"name,omitempty"`
	TrackCount  int             `json:"trackCount"`
	Clips       []clipOut       `json:"clips"`
	Timemarkers []timeMarkerOut `json:"timemarkers"`
}

// ProjectSummary is the bridge's flat project summary.
type ProjectSummary struct {
	PPQ          int                  `json:"ppq"`
	Tempo        *float64             `json:"tempo,omitempty"`
	Channels     []ChannelSummary     `json:"channels"`
	Inserts      []InsertSummary      `json:"inserts"`
	Patterns     []PatternSummary     `json:"patterns"`
	Arrangements []ArrangementSummary `json:"arrangements"`
}

func rgbaPtrOut(c *RGBA) *rgbaOut {
	if c == nil {
		return nil
	}
	return &rgbaOut{c.R, c.G, c.B, c.A}
}

func f32(v float32) float64 { return float64(v) }

// BuildProjectSummary builds the flat summary used by the bridge's list_* kinds.
func BuildProjectSummary(p *FLPProject) ProjectSummary {
	s := ProjectSummary{
		PPQ: p.Header.PPQ, Tempo: GetTempo(p),
		Channels: []ChannelSummary{}, Inserts: []InsertSummary{}, Patterns: []PatternSummary{}, Arrangements: []ArrangementSummary{},
	}
	for _, ch := range p.Channels {
		o := ChannelSummary{Iid: ch.Iid, Kind: string(ch.Kind), Name: ch.Name, SamplePath: ch.SamplePath,
			Color: rgbaPtrOut(ch.Color), Enabled: ch.Enabled, PingPongLoop: ch.PingPongLoop, Locked: ch.Locked, Zipped: ch.Zipped}
		if ch.Plugin != nil {
			o.Plugin = &pluginSummaryOut{InternalName: ch.Plugin.InternalName, Name: ch.Plugin.Name, Vendor: ch.Plugin.Vendor}
		}
		if ch.Levels != nil {
			l := ch.Levels
			o.Levels = &levelsOut{l.Pan, l.Volume, l.PitchShift, l.FilterModX, l.FilterModY, l.FilterType}
		}
		s.Channels = append(s.Channels, o)
	}
	for _, ins := range p.Inserts {
		o := InsertSummary{Index: ins.Index, Name: ins.Name, Color: rgbaPtrOut(ins.Color), Icon: ins.Icon,
			Output: ins.Output, Input: ins.Input, Slots: []SlotSummary{}}
		if ins.Flags != nil {
			f := ins.Flags
			o.Flags = &insertFlagsOut{f.PolarityReversed, f.SwapLeftRight, f.EnableEffects, f.Enabled, f.DisableThreadedProcessing,
				f.DockMiddle, f.DockRight, f.SeparatorShown, f.Locked, f.Solo, f.AudioTrack}
		}
		for _, sl := range ins.Slots {
			so := SlotSummary{Index: sl.Index, PluginName: sl.PluginName, HasPlugin: sl.HasPlugin}
			if sl.HasPlugin != nil && *sl.HasPlugin {
				name := "Unknown"
				switch {
				case sl.PluginVstName != nil:
					name = *sl.PluginVstName
				case sl.PluginName != nil:
					name = *sl.PluginName
				case sl.InternalName != nil:
					name = *sl.InternalName
				}
				so.Plugin = &slotPluginOut{Name: name, Vendor: sl.PluginVendor}
			}
			o.Slots = append(o.Slots, so)
		}
		s.Inserts = append(s.Inserts, o)
	}
	for _, pt := range p.Patterns {
		o := PatternSummary{ID: pt.ID, Name: pt.Name, Length: pt.Length, Color: rgbaPtrOut(pt.Color), Looped: pt.Looped,
			Notes: append([]Note{}, pt.Notes...), Controllers: []controllerOut{}}
		for _, c := range pt.Controllers {
			o.Controllers = append(o.Controllers, controllerOut{c.Position, c.Channel, c.Flags, f32(c.Value)})
		}
		s.Patterns = append(s.Patterns, o)
	}
	for _, a := range p.Arrangements {
		o := ArrangementSummary{ID: a.ID, Name: a.Name, TrackCount: len(a.Tracks), Clips: []clipOut{}, Timemarkers: []timeMarkerOut{}}
		for _, c := range a.Clips {
			o.Clips = append(o.Clips, clipOut{c.Position, c.ItemIndex, c.Length, c.TrackRvidx, c.Group, c.ItemFlags, f32(c.StartOffset), f32(c.EndOffset)})
		}
		for _, m := range a.TimeMarkers {
			o.Timemarkers = append(o.Timemarkers, timeMarkerOut{string(m.Kind), m.Position, m.Name, m.Numerator, m.Denominator})
		}
		s.Arrangements = append(s.Arrangements, o)
	}
	return s
}


