package dsp

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/audio"
)

func TestFFTFindsASine(t *testing.T) {
	const n = 1024
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Sin(2*math.Pi*50*float64(i)/n), 0)
	}
	if err := FFT(x); err != nil {
		t.Fatal(err)
	}
	best, bestV := 0, 0.0
	for k := 0; k < n/2; k++ {
		if m := real(x[k])*real(x[k]) + imag(x[k])*imag(x[k]); m > bestV {
			best, bestV = k, m
		}
	}
	if best != 50 {
		t.Errorf("peak at bin %d, want 50", best)
	}
}

func TestFFTRefusesAWrongLength(t *testing.T) {
	if err := FFT(make([]complex128, 100)); err == nil {
		t.Error("accepted 100 samples")
	}
}

func sineWAV(t *testing.T, dir string, hz float64, seconds float64, rate, channels int) string {
	t.Helper()
	n := int(seconds * float64(rate))
	data := make([]int32, n*channels)
	for i := 0; i < n; i++ {
		v := int32(0.5 * math.MaxInt32 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
		for c := 0; c < channels; c++ {
			data[i*channels+c] = v
		}
	}
	pick := make([]int, channels)
	for i := range pick {
		pick[i] = i
	}
	p := filepath.Join(dir, "sine.wav")
	if _, err := audio.WriteWAV(p, data, channels, pick, rate); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMonoRate(t *testing.T) {
	for _, c := range []struct{ in, rate, factor int }{
		{48000, 12000, 4}, {44100, 11025, 4}, {96000, 12000, 8}, {8000, 8000, 1},
	} {
		if r, f := MonoRate(c.in); r != c.rate || f != c.factor {
			t.Errorf("MonoRate(%d) = %d,%d want %d,%d", c.in, r, f, c.rate, c.factor)
		}
	}
}

func TestReadMonoGivesTheToneAtTheLowerRate(t *testing.T) {
	p := sineWAV(t, t.TempDir(), 440, 2, 48000, 2)
	st, err := NewSTFT(2048, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var rate int
	var peaks []int
	info, rate, err := ReadMono(p, func(b []float32) error {
		st.Push(b, func(mag []float64, _ int64) {
			best := 1
			for k := 1; k < len(mag); k++ {
				if mag[k] > mag[best] {
					best = k
				}
			}
			peaks = append(peaks, best)
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rate != 12000 || info.Channels != 2 {
		t.Fatalf("rate %d, channels %d", rate, info.Channels)
	}
	if len(peaks) < 5 {
		t.Fatalf("only %d frames", len(peaks))
	}
	want := int(math.Round(440 * 2048 / 12000.0))
	for _, p := range peaks {
		if p < want-1 || p > want+1 {
			t.Errorf("peak bin %d, want %d (440 Hz at 12 kHz / 2048)", p, want)
		}
	}
}

func TestSTFTIsTheSameInAnyBlockSize(t *testing.T) {
	sig := make([]float32, 10000)
	for i := range sig {
		sig[i] = float32(math.Sin(float64(i) * 0.3))
	}
	run := func(block int) [][]float64 {
		s, _ := NewSTFT(512, 256)
		var out [][]float64
		for i := 0; i < len(sig); i += block {
			e := i + block
			if e > len(sig) {
				e = len(sig)
			}
			s.Push(sig[i:e], func(mag []float64, _ int64) { out = append(out, append([]float64(nil), mag...)) })
		}
		return out
	}
	a, b := run(10000), run(333)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("frames %d vs %d", len(a), len(b))
	}
	for i := range a {
		for k := range a[i] {
			if math.Abs(a[i][k]-b[i][k]) > 1e-9 {
				t.Fatalf("frame %d bin %d differs", i, k)
			}
		}
	}
}

func TestSTFTFirstSampleAdvancesByHop(t *testing.T) {
	s, _ := NewSTFT(8, 4)
	var at []int64
	s.Push(make([]float32, 20), func(_ []float64, first int64) { at = append(at, first) })
	want := []int64{0, 4, 8, 12}
	if len(at) != len(want) {
		t.Fatalf("at = %v", at)
	}
	for i := range want {
		if at[i] != want[i] {
			t.Errorf("at = %v, want %v", at, want)
		}
	}
}

func TestRMSAndDB(t *testing.T) {
	if r := RMS([]float32{1, -1, 1, -1}); math.Abs(r-1) > 1e-9 {
		t.Errorf("rms = %v", r)
	}
	if RMS(nil) != 0 || DB(0) != -120 || math.Abs(DB(1)) > 1e-9 {
		t.Error("RMS/DB edges")
	}
}
