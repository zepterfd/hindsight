package audio

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// A run of silence is a stretch of a take, at least MinSeconds long, in which
// no channel gets louder than a threshold. Takes are kept from a buffer that
// was always listening, so a long gap in one means the band stopped for a
// drink, and it is the part nobody wants to scrub through.

const (
	// silenceWindow is the length each level is measured over.
	silenceWindow = 0.02
	// A threshold found for a take is never above or below these, in dBFS:
	// above the first would call quiet playing silence, below the second
	// would call a noisy room (hiss, a fan) never silent.
	silenceCeiling = -45.0
	silenceFloor   = -75.0
	// The noise floor is the quietest tenth of the take's windows; silence
	// is a little above it, as a room's hiss moves about.
	silenceMargin = 10.0
)

// SilenceOptions says what counts as silence and what to leave behind.
type SilenceOptions struct {
	// MinSeconds is the shortest silence worth removing. 15 by default: a
	// gap that long is a pause in the session, and shorter ones are part of
	// the music.
	MinSeconds float64
	// KeepSeconds is how much of each removed silence is left, half on each
	// side of the join, so the sound before it can ring out and the sound
	// after it has its pre-roll. 1 by default.
	KeepSeconds float64
	// ThresholdDB, if it is below zero, is the level under which a window is
	// silent. Left at zero, one is found from the take itself.
	ThresholdDB float64
}

func (o SilenceOptions) withDefaults() SilenceOptions {
	if o.MinSeconds <= 0 {
		o.MinSeconds = 15
	}
	if o.KeepSeconds <= 0 {
		o.KeepSeconds = 1
	}
	if o.KeepSeconds > o.MinSeconds {
		o.KeepSeconds = o.MinSeconds
	}
	return o
}

// SilenceReport is where a take's silences are.
type SilenceReport struct {
	Runs        []Range `json:"runs"`
	ThresholdDB float64 `json:"threshold_db"`
	// RemovedSeconds is how much shorter the take would be, the silences
	// less what is kept of each.
	RemovedSeconds float64 `json:"removed_seconds"`
	Seconds        float64 `json:"seconds"`
}

// FindSilence reads a take and reports its silences.
func FindSilence(path string, opt SilenceOptions) (SilenceReport, error) {
	opt = opt.withDefaults()
	info, err := ReadWAVInfo(path)
	if err != nil {
		return SilenceReport{}, err
	}
	if info.BitsPerSample != 32 {
		return SilenceReport{}, ErrBitDepth
	}
	frames := info.Frames()
	rep := SilenceReport{Runs: []Range{}, Seconds: info.Duration()}
	if frames == 0 {
		return rep, nil
	}
	win := int64(math.Max(1, silenceWindow*float64(info.SampleRate)))
	levels := make([]float64, 0, frames/win+1)
	ch := int64(info.Channels)
	var peak int32
	var inWin int64
	if _, err := ReadFrames(path, 0, frames, 1<<15, func(block []int32, _ int64) error {
		for i := 0; i+int(ch) <= len(block); i += int(ch) {
			for c := 0; c < int(ch); c++ {
				v := block[i+c]
				if v < 0 {
					v = -v
				}
				if v < 0 { // -2^31 negated
					v = math.MaxInt32
				}
				if v > peak {
					peak = v
				}
			}
			inWin++
			if inWin == win {
				levels = append(levels, dbOf(peak))
				peak, inWin = 0, 0
			}
		}
		return nil
	}); err != nil {
		return rep, err
	}
	if inWin > 0 {
		levels = append(levels, dbOf(peak))
	}

	thr := opt.ThresholdDB
	if thr >= 0 {
		thr = noiseThreshold(levels)
	}
	rep.ThresholdDB = math.Round(thr*10) / 10

	minWin := int(math.Ceil(opt.MinSeconds / silenceWindow))
	keepFrames := int64(opt.KeepSeconds * float64(info.SampleRate))
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= minWin {
			r := Range{Start: int64(start) * win, End: int64(end) * win}
			if r.End > frames {
				r.End = frames
			}
			rep.Runs = append(rep.Runs, r)
			if r.Frames() > keepFrames {
				rep.RemovedSeconds += float64(r.Frames()-keepFrames) / float64(info.SampleRate)
			}
		}
		start = -1
	}
	for i, l := range levels {
		if l < thr {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(levels))
	rep.RemovedSeconds = math.Round(rep.RemovedSeconds*100) / 100
	return rep, nil
}

func dbOf(peak int32) float64 {
	if peak <= 0 {
		return -120
	}
	return 20 * math.Log10(float64(peak)/2147483648.0)
}

// noiseThreshold is a little above the take's quietest tenth, kept within
// the ceiling and the floor.
func noiseThreshold(levels []float64) float64 {
	c := append([]float64(nil), levels...)
	sort.Float64s(c)
	floor := c[len(c)/10]
	return math.Max(silenceFloor, math.Min(silenceCeiling, floor+silenceMargin))
}

// keepRanges are what is left of a take when each silence is cut down to
// KeepSeconds: the audio between the runs, each run's first and last half of
// KeepSeconds staying with its neighbours.
func keepRanges(runs []Range, frames int64, keepFrames int64) []Range {
	var out []Range
	at := int64(0)
	for _, r := range runs {
		if r.Frames() <= keepFrames {
			continue
		}
		head := keepFrames / 2
		tail := keepFrames - head
		if end := r.Start + head; end > at {
			out = append(out, Range{at, end})
		}
		at = r.End - tail
	}
	if at < frames {
		out = append(out, Range{at, frames})
	}
	return out
}

// ErrNoSilence is a take with nothing to remove.
var ErrNoSilence = fmt.Errorf("no silence to remove")

// TrimSilence writes a new take with the take's long silences cut out
// (CutSegments), and reports what it removed. The source is untouched.
func TrimSilence(dir, name string, opt SilenceOptions, now time.Time) (string, SilenceReport, error) {
	opt = opt.withDefaults()
	path := dir + "/" + name
	rep, err := FindSilence(path, opt)
	if err != nil {
		return "", rep, err
	}
	if len(rep.Runs) == 0 || rep.RemovedSeconds <= 0 {
		return "", rep, ErrNoSilence
	}
	info, err := ReadWAVInfo(path)
	if err != nil {
		return "", rep, err
	}
	segs := keepRanges(rep.Runs, info.Frames(), int64(opt.KeepSeconds*float64(info.SampleRate)))
	label := ""
	if m := ReadMeta(path); true {
		base := cutLabelBase(m.Label)
		if base == "" {
			base = name[:len(name)-len(".wav")]
		}
		label = base + " · no gaps"
	}
	out, err := CutSegments(dir, SegmentsRequest{Source: name, Segments: segs, Label: label}, now)
	return out, rep, err
}
