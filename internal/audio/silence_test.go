package audio

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

const silenceRate = 8000

// writeGapTake writes a stereo take of: loud seconds of tone, gap seconds of
// a faint hiss, then loud seconds of tone.
func writeGapTake(t *testing.T, dir, name string, loud, gap float64) string {
	t.Helper()
	path := filepath.Join(dir, name)
	n := int(loud*silenceRate)*2 + int(gap*silenceRate)
	data := make([]int32, 0, n*2)
	tone := func(frames int, from int) {
		for i := 0; i < frames; i++ {
			v := int32(0.3 * math.MaxInt32 * math.Sin(2*math.Pi*220*float64(from+i)/silenceRate))
			data = append(data, v, v)
		}
	}
	tone(int(loud*silenceRate), 0)
	for i := 0; i < int(gap*silenceRate); i++ {
		h := int32(((i * 7919) % 2001) - 1000) // about -110 dBFS
		data = append(data, h*1000, h*1000)
	}
	tone(int(loud*silenceRate), 0)
	if _, err := WriteWAV(path, data, 2, []int{0, 1}, silenceRate); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindSilenceFindsOnlyTheLongGap(t *testing.T) {
	dir := t.TempDir()
	path := writeGapTake(t, dir, "gap.wav", 5, 20)
	rep, err := FindSilence(path, SilenceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 1 {
		t.Fatalf("runs = %+v, want one", rep.Runs)
	}
	r := rep.Runs[0]
	wantStart, wantEnd := int64(5*silenceRate), int64(25*silenceRate)
	if d := r.Start - wantStart; d < -silenceRate/10 || d > silenceRate/10 {
		t.Errorf("start = %d, want about %d", r.Start, wantStart)
	}
	if d := r.End - wantEnd; d < -silenceRate/10 || d > silenceRate/10 {
		t.Errorf("end = %d, want about %d", r.End, wantEnd)
	}
	if rep.RemovedSeconds < 18.5 || rep.RemovedSeconds > 19.5 {
		t.Errorf("removed = %v, want about 19", rep.RemovedSeconds)
	}
	if rep.ThresholdDB > silenceCeiling || rep.ThresholdDB < silenceFloor {
		t.Errorf("threshold %v outside [%v, %v]", rep.ThresholdDB, silenceFloor, silenceCeiling)
	}
}

func TestFindSilenceIgnoresAGapShorterThanTheMinimum(t *testing.T) {
	dir := t.TempDir()
	path := writeGapTake(t, dir, "short.wav", 5, 10)
	rep, err := FindSilence(path, SilenceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 0 {
		t.Errorf("a 10 s gap is under the 15 s default: %+v", rep.Runs)
	}
	rep, err = FindSilence(path, SilenceOptions{MinSeconds: 5})
	if err != nil || len(rep.Runs) != 1 {
		t.Errorf("min 5 s should find it: %+v %v", rep.Runs, err)
	}
}

func TestFindSilenceRespectsAGivenThreshold(t *testing.T) {
	dir := t.TempDir()
	path := writeGapTake(t, dir, "thr.wav", 5, 20)
	// A threshold below the hiss finds nothing: the hiss is not under it.
	rep, err := FindSilence(path, SilenceOptions{ThresholdDB: -140})
	if err != nil || len(rep.Runs) != 0 {
		t.Errorf("runs = %+v err = %v, want none", rep.Runs, err)
	}
}

func TestKeepRangesLeavesKeepSecondsOfEachGap(t *testing.T) {
	const rate = 1000
	runs := []Range{{5000, 25000}}
	got := keepRanges(runs, 30000, 1000)
	want := []Range{{0, 5500}, {24500, 30000}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("keepRanges = %+v, want %+v", got, want)
	}
	// A run no longer than what is kept is left whole.
	got = keepRanges([]Range{{5000, 5800}}, 30000, 1000)
	if len(got) != 1 || got[0] != (Range{0, 30000}) {
		t.Errorf("short run: %+v", got)
	}
	_ = rate
}

func TestTrimSilenceWritesAShorterTakeAndLeavesTheSource(t *testing.T) {
	dir := t.TempDir()
	path := writeGapTake(t, dir, "gap.wav", 5, 20)
	srcInfo, _ := ReadWAVInfo(path)
	if err := WriteMeta(path, Meta{Version: MetaVersion, Label: "jam",
		Flags: []Flag{{Frame: 2 * silenceRate, Label: "head"}, {Frame: 15 * silenceRate, Label: "gap"},
			{Frame: 27 * silenceRate, Label: "tail"}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	name, rep, err := TrimSilence(dir, "gap.wav", SilenceOptions{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	out := filepath.Join(dir, name)
	info, err := ReadWAVInfo(out)
	if err != nil {
		t.Fatal(err)
	}
	// 30 s of 30 s + 20 s gap -> about 11 s: 10 s of sound and ~1 s kept.
	if got := info.Duration(); got < 10.8 || got > 11.2 {
		t.Errorf("duration = %v, want about 11", got)
	}
	if after, _ := ReadWAVInfo(path); after.Frames() != srcInfo.Frames() {
		t.Error("the source take was changed")
	}
	m := ReadMeta(out)
	if m.Label != "jam · no gaps" {
		t.Errorf("label = %q", m.Label)
	}
	// The flag in the gap has no audio left to mark; the others move.
	if len(m.Flags) != 2 {
		t.Fatalf("flags = %+v, want head and tail", m.Flags)
	}
	if m.Flags[0].Frame != 2*silenceRate {
		t.Errorf("head moved to %d", m.Flags[0].Frame)
	}
	wantTail := int64(27*silenceRate) - int64(19*silenceRate) // 19 s of the 20 s gap is gone before it
	if d := m.Flags[1].Frame - wantTail; d < -silenceRate/10 || d > silenceRate/10 {
		t.Errorf("tail at %d, want about %d", m.Flags[1].Frame, wantTail)
	}
	if m.DownbeatFrame != nil {
		t.Error("a take with a hole has no downbeat to carry")
	}
}

func TestTrimSilenceWithNothingToRemoveIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeGapTake(t, dir, "short.wav", 5, 3)
	if _, _, err := TrimSilence(dir, "short.wav", SilenceOptions{}, time.Now()); err != ErrNoSilence {
		t.Errorf("err = %v, want ErrNoSilence", err)
	}
}

func TestCutSegmentsJoinsRegionsInOrder(t *testing.T) {
	dir := t.TempDir()
	writeTestTake(t, dir, "src.wav", 48000)
	fade := FadeFrames(48000)
	name, err := CutSegments(dir, SegmentsRequest{Source: "src.wav",
		Segments: []Range{{1000, 5000}, {20000, 30000}}}, time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name)
	info, err := ReadWAVInfo(out)
	if err != nil || info.Frames() != 14000 {
		t.Fatalf("info = %+v err = %v", info, err)
	}
	var atJoin, afterJoin int32
	if _, err := ReadFrames(out, 0, 14000, 14000, func(b []int32, _ int64) error {
		atJoin = b[(4000-int(fade)-1)*2]
		afterJoin = b[(4000+int(fade))*2]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := int32(1000+4000-int(fade)-1) << 10; atJoin != want {
		t.Errorf("before the join = %d, want %d", atJoin, want)
	}
	if want := int32(20000+int(fade)) << 10; afterJoin != want {
		t.Errorf("after the join = %d, want %d", afterJoin, want)
	}
}

func TestCutSegmentsRejectsOverlapAndShortRegions(t *testing.T) {
	dir := t.TempDir()
	writeTestTake(t, dir, "src.wav", 48000)
	now := time.Now()
	cases := map[string][]Range{
		"overlap": {{1000, 5000}, {4000, 9000}},
		"empty":   nil,
		"short":   {{1000, 1010}},
		"past":    {{1000, 5000}, {90000, 999999}},
	}
	for name, segs := range cases {
		if _, err := CutSegments(dir, SegmentsRequest{Source: "src.wav", Segments: segs}, now); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestMoveIntoMapsFramesPastGaps(t *testing.T) {
	segs := []Range{{100, 200}, {500, 700}}
	for _, c := range []struct {
		in, out int64
		ok      bool
	}{{99, 0, false}, {100, 0, true}, {199, 99, true}, {200, 0, false}, {500, 100, true}, {699, 299, true}, {700, 0, false}} {
		got, ok := moveInto(segs, c.in)
		if ok != c.ok || (ok && got != c.out) {
			t.Errorf("moveInto(%d) = %d, %v; want %d, %v", c.in, got, ok, c.out, c.ok)
		}
	}
}
