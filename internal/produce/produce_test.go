package produce

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `[Parsed_loudnorm_0 @ 0x1] 
{
	"input_i" : "-27.50",
	"input_tp" : "-9.10",
	"input_lra" : "3.20",
	"input_thresh" : "-38.00",
	"output_i" : "-23.10",
	"output_tp" : "-4.20",
	"output_lra" : "2.00",
	"output_thresh" : "-33.60",
	"normalization_type" : "dynamic",
	"target_offset" : "0.10"
}
`

func TestParseLoudness(t *testing.T) {
	l, err := ParseLoudness("frame= 1 size=N/A\n" + sample)
	if err != nil {
		t.Fatal(err)
	}
	if l.Integrated != -27.5 || l.TruePeak != -9.1 || l.Range != 3.2 || l.Threshold != -38 || l.Offset != 0.1 {
		t.Fatalf("%+v", l)
	}
	for _, bad := range []string{"", "no json", `{"input_i": "x"}`, `{"input_i": "-20"}`} {
		if _, err := ParseLoudness(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	// ffmpeg writes "-inf" for silence; that parses, and is handled by the stem filter.
	l, err = ParseLoudness(strings.ReplaceAll(sample, `"-27.50"`, `"-inf"`))
	if err != nil || !math.IsInf(l.Integrated, -1) {
		t.Fatalf("-inf: %+v %v", l, err)
	}
}

func TestValidate(t *testing.T) {
	ok := Plan{Inputs: []string{"a.wav"}, Output: "o.flac"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]Plan{
		"no inputs":     {Output: "o.wav"},
		"bad extension": {Inputs: []string{"a.wav"}, Output: "o.ogg"},
		"overwrites":    {Inputs: []string{"a.wav"}, Output: "a.wav"},
		"loud target":   {Inputs: []string{"a.wav"}, Output: "o.wav", TargetLUFS: -2},
		"peak above 0":  {Inputs: []string{"a.wav"}, Output: "o.wav", TruePeakDB: 1},
		"negative fade": {Inputs: []string{"a.wav"}, Output: "o.wav", FadeIn: -1},
	} {
		if p.Validate() == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMixLevelsEachStemToTheStemTarget(t *testing.T) {
	p := Plan{Inputs: []string{"bass.wav", "gtr.wav"}, Output: "o.wav", Gains: map[int]float64{1: -2}}
	got := p.Mix([]Loudness{{Integrated: -30}, {Integrated: -20}})
	for _, want := range []string{
		"[0:a]highpass=f=30,volume=7dB[s0]",  // -23 - -30
		"[1:a]highpass=f=30,volume=-5dB[s1]", // -23 - -20 = -3, trim -2
		"[s0][s1]amix=inputs=2:normalize=0:duration=longest[mix]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestStemGainIsCappedAndSilenceLeftAlone(t *testing.T) {
	p := Plan{Inputs: []string{"a.wav", "b.wav", "c.wav"}, Output: "o.wav", HighpassHz: -1}
	got := p.Mix([]Loudness{{Integrated: -60}, {Integrated: math.Inf(-1)}, {Integrated: -5}})
	for _, want := range []string{"[0:a]volume=18dB[s0]", "[1:a]volume=0dB[s1]", "[2:a]volume=-18dB[s2]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "highpass") {
		t.Error("highpass should be off")
	}
}

func TestOneStemIsNotMixed(t *testing.T) {
	got := Plan{Inputs: []string{"a.wav"}, Output: "o.wav"}.Mix([]Loudness{{Integrated: -23}})
	if strings.Contains(got, "amix") || !strings.Contains(got, "[s0]anull[mix]") {
		t.Fatal(got)
	}
}

func TestMasterMeasuresThenAppliesLinearly(t *testing.T) {
	p := Plan{Inputs: []string{"a.wav"}, Output: "o.wav", FadeIn: 1, FadeOut: 3, TargetLUFS: -16, TruePeakDB: -1.5}
	if m := p.Master(nil, 0); m != "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json" {
		t.Fatal(m)
	}
	m := p.Master(&Loudness{Integrated: -20, TruePeak: -3, Range: 4, Threshold: -30, Offset: 0.5}, 100)
	for _, want := range []string{"measured_I=-20", "linear=true", "afade=t=in:d=1", "afade=t=out:st=97:d=3", "alimiter=limit=0.841"} {
		if !strings.Contains(m, want) {
			t.Errorf("missing %q in %s", want, m)
		}
	}
	// A fade longer than the song is dropped, not made negative.
	if m := p.Master(&Loudness{}, 2); strings.Contains(m, "afade=t=out") {
		t.Errorf("fade-out longer than the song: %s", m)
	}
}

func TestArgsByExtension(t *testing.T) {
	st := []Loudness{{Integrated: -23}}
	for ext, codec := range map[string]string{".wav": "pcm_s24le", ".flac": "flac", ".mp3": "libmp3lame"} {
		p := Plan{Inputs: []string{"a.wav"}, Output: "o" + ext}
		args := strings.Join(p.Args(st, Loudness{}, 10), " ")
		if !strings.Contains(args, "-c:a "+codec) || !strings.HasSuffix(args, "-y o"+ext) {
			t.Errorf("%s: %s", ext, args)
		}
	}
}

// wavSine writes a 32-bit mono WAV like a take, a sine at the given amplitude.
func wavSine(t *testing.T, dir, name string, seconds int, amp float64, hz float64) string {
	t.Helper()
	const rate = 48000
	n := seconds * rate
	pcm := make([]byte, n*4)
	for i := 0; i < n; i++ {
		v := int32(amp * 2147483647 * math.Sin(2*math.Pi*hz*float64(i)/rate))
		binary.LittleEndian.PutUint32(pcm[i*4:], uint32(v))
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*4)
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 32)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, append(h, pcm...), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunSequencesTheStepsAndStopsOnError(t *testing.T) {
	dir := t.TempDir()
	a, b := wavSine(t, dir, "a.wav", 1, 0.1, 220), wavSine(t, dir, "b.wav", 2, 0.4, 330)
	var calls [][]string
	run := func(_ context.Context, args []string) (string, error) {
		calls = append(calls, args)
		return sample, nil
	}
	var said []string
	rep, err := Run(context.Background(), Plan{Inputs: []string{a, b}, Output: filepath.Join(dir, "o.wav")}, run, func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || rep.Duration != 2 || len(rep.Stems) != 2 {
		t.Fatalf("%d calls, %+v", len(calls), rep)
	}
	if !strings.Contains(strings.Join(calls[2], " "), "print_format=json") || strings.Contains(strings.Join(calls[3], " "), "print_format") {
		t.Error("the third call measures, the fourth renders")
	}
	if len(said) != 4 || !strings.HasPrefix(said[0], "Measuring a.wav") {
		t.Errorf("progress: %v", said)
	}

	boom := errors.New("boom")
	n := 0
	_, err = Run(context.Background(), Plan{Inputs: []string{a}, Output: filepath.Join(dir, "o.wav")}, func(context.Context, []string) (string, error) {
		n++
		return "", boom
	}, nil)
	if !errors.Is(err, boom) || n != 1 {
		t.Fatalf("err %v after %d calls", err, n)
	}
	if _, err := Run(context.Background(), Plan{Inputs: []string{filepath.Join(dir, "missing.wav")}, Output: "o.wav"}, run, nil); err == nil {
		t.Fatal("a missing input should fail before ffmpeg runs")
	}
}

// The real thing: two sines far apart in level come out at the target.
func TestEndToEndWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	a := wavSine(t, dir, "quiet.wav", 6, 0.01, 110)
	b := wavSine(t, dir, "loud.wav", 6, 0.5, 440)
	out := filepath.Join(dir, "song.flac")
	rep, err := Run(context.Background(), Plan{Inputs: []string{a, b}, Output: out, TargetLUFS: -16, FadeIn: 0.5, FadeOut: 0.5}, FFmpeg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Stems[0].Integrated > -40 || rep.Stems[1].Integrated < -15 {
		t.Fatalf("test stems should differ widely: %+v", rep.Stems)
	}
	stderr, err := FFmpeg(context.Background(), MeasureArgs(out))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseLoudness(stderr)
	if err != nil {
		t.Fatal(err)
	}
	// The fades cost a little of a six-second song's integrated level.
	if math.Abs(got.Integrated-(-16)) > 1.5 {
		t.Errorf("integrated %.2f LUFS, want about -16", got.Integrated)
	}
	if got.TruePeak > -0.9 {
		t.Errorf("true peak %.2f dBTP is over the -1 ceiling", got.TruePeak)
	}
}
