package sem

import "sort"

// ResetIntervals derives the ordered, merged reset-active intervals used by
// "disable iff". Signal-derived intervals and manual intervals are unioned;
// neither source overwrites the other (both remain inspectable).
func (w *Wave) ResetIntervals(spec ResetSpec) []ResetInterval {
	var out []ResetInterval
	if spec.Signal != "" {
		out = append(out, w.signalReset(spec)...)
	}
	out = append(out, spec.Manual...)
	return mergeIntervals(out)
}

func (w *Wave) signalReset(spec ResetSpec) []ResetInterval {
	sv := w.Signals[spec.Signal]
	if sv == nil {
		return nil
	}
	var iv []ResetInterval
	var start *int64
	for _, p := range sv.Points {
		cur := bitOf(p)
		active := false
		if spec.ActiveHigh {
			active = cur == bit1
		} else {
			active = cur == bit0
		}
		if active && start == nil {
			t := p.TimeFS
			start = &t
		}
		if !active && start != nil {
			iv = append(iv, ResetInterval{StartFS: *start, EndFS: p.TimeFS, Active: true, Source: "signal", Note: "derived from reset level"})
			start = nil
		}
	}
	if start != nil {
		end := sv.Points[len(sv.Points)-1].TimeFS
		iv = append(iv, ResetInterval{StartFS: *start, EndFS: end, Active: true, Source: "signal", Note: "reset active to end of trace"})
	}
	return iv
}

func mergeIntervals(in []ResetInterval) []ResetInterval {
	if len(in) == 0 {
		return nil
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].StartFS < in[j].StartFS })
	out := []ResetInterval{in[0]}
	for _, v := range in[1:] {
		last := &out[len(out)-1]
		if v.StartFS <= last.EndFS {
			if v.EndFS > last.EndFS {
				last.EndFS = v.EndFS
			}
			if last.Source != v.Source {
				last.Source = "union"
			}
			continue
		}
		out = append(out, v)
	}
	return out
}

// InReset reports whether absolute time t lies in a reset interval.
func InReset(iv []ResetInterval, t int64) bool {
	i := sort.Search(len(iv), func(i int) bool { return iv[i].EndFS >= t })
	return i < len(iv) && iv[i].StartFS <= t && t <= iv[i].EndFS
}
