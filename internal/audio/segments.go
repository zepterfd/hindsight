package audio

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Range is frames [Start, End) of a take.
type Range struct {
	Start int64 `json:"start_frame"`
	End   int64 `json:"end_frame"`
}

// Frames is the range's length.
func (r Range) Frames() int64 { return r.End - r.Start }

// ErrSegments is a list of ranges that overlap, run backwards or are empty.
var ErrSegments = errors.New("segments must be in order, apart, and not empty")

// SegmentsRequest asks for several regions of one source take, joined in
// order into one new take.
type SegmentsRequest struct {
	Source   string  // filename of the source take
	Segments []Range // in order, apart, each at least two fades long
	Label    string  // optional; defaults to "<source label or stem> · <span>"
}

// CutSegments writes the regions of a take, one after another, as a new take:
// each with the cut's declick fades at both edges, so a join never clicks,
// with the source's tempo and lane choices, and its flags and dropouts that
// fall inside a region moved to where that region now lies. The source is
// never modified. It is what removing the silence from a take is: keep
// everything but the gaps.
//
// A take with a hole in it has no bar grid to carry (the bars after the hole
// move), so unless there is one region the downbeat is not carried, and the
// MIDI beside the source is not either: both are positions on the old
// timeline. One region is exactly a Cut.
func CutSegments(dir string, req SegmentsRequest, now time.Time) (string, error) {
	srcPath := filepath.Join(dir, filepath.Base(req.Source))
	info, err := ReadWAVInfo(srcPath)
	if err != nil {
		return "", err
	}
	if info.BitsPerSample != 32 {
		return "", ErrBitDepth
	}
	if len(req.Segments) == 0 {
		return "", ErrSegments
	}
	fade := FadeFrames(info.SampleRate)
	var total int64
	for i, s := range req.Segments {
		if s.Start < 0 || s.End <= s.Start || s.End > info.Frames() {
			return "", fmt.Errorf("%w: [%d, %d) of %d frames", ErrRange, s.Start, s.End, info.Frames())
		}
		if i > 0 && s.Start < req.Segments[i-1].End {
			return "", ErrSegments
		}
		if s.Frames() < 2*fade+1 {
			return "", ErrTooShort
		}
		total += s.Frames()
	}
	ch := info.Channels

	srcMeta := ReadMeta(srcPath)
	first, last := req.Segments[0], req.Segments[len(req.Segments)-1]
	m := Meta{Version: MetaVersion, Label: strings.TrimSpace(req.Label)}
	if m.Label == "" {
		base := cutLabelBase(srcMeta.Label)
		if base == "" {
			base = strings.TrimSuffix(filepath.Base(req.Source), ".wav")
		}
		m.Label = base + " · " + spanLabel(first.Start, last.End, info.SampleRate)
	}
	if srcMeta.BPM != nil {
		bpm := *srcMeta.BPM
		m.BPM = &bpm
		m.TempoFrom = srcMeta.TempoFrom
	}
	if len(srcMeta.LaneKinds) > 0 {
		m.LaneKinds = make(map[string]string, len(srcMeta.LaneKinds))
		for k, v := range srcMeta.LaneKinds {
			m.LaneKinds[k] = v
		}
	}
	if len(req.Segments) == 1 {
		if db, ok := cutDownbeat(srcMeta, first.Start, first.End, info.SampleRate); ok {
			m.DownbeatFrame = &db
		}
	}
	m.Source = &CutSource{Name: filepath.Base(req.Source), StartFrame: first.Start, EndFrame: last.End}

	var flags []Flag
	for _, f := range srcMeta.Flags {
		if at, ok := moveInto(req.Segments, f.Frame); ok {
			flags = append(flags, Flag{Frame: at, Label: f.Label})
		}
	}
	for _, d := range srcMeta.Dropouts {
		if at, ok := moveInto(req.Segments, d); ok && at > 0 {
			m.Dropouts = append(m.Dropouts, at)
		}
	}

	out, err := WriteTake(dir, now, total, ch, info.SampleRate, func(write func([]int32) error) error {
		for _, s := range req.Segments {
			segTotal := s.Frames()
			if _, err := ReadFrames(srcPath, s.Start, s.End, 1<<14, func(block []int32, firstFrame int64) error {
				applyFades(block, firstFrame-s.Start, segTotal, ch, fade)
				return write(block)
			}); err != nil {
				return err
			}
		}
		return nil
	}, m)
	if err != nil {
		return "", err
	}
	stampFlagsAt(filepath.Join(dir, out), filepath.Join(dir, out), flags)
	return out, nil
}

// moveInto maps a frame of the source to where it lies in the joined take,
// or false when no region holds it.
func moveInto(segs []Range, frame int64) (int64, bool) {
	var before int64
	for _, s := range segs {
		if frame >= s.Start && frame < s.End {
			return before + frame - s.Start, true
		}
		before += s.Frames()
	}
	return 0, false
}

// SortedSegments returns the ranges in order, or ErrSegments if any overlap.
func SortedSegments(in []Range) ([]Range, error) {
	out := append([]Range(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	for i, r := range out {
		if r.End <= r.Start || (i > 0 && r.Start < out[i-1].End) {
			return nil, ErrSegments
		}
	}
	return out, nil
}
