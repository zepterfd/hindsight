// Package dsp is the small amount of signal processing that more than one
// analyser needs, in one place so no analyser carries a copy of it: reading a
// take as a mono stream at a lower rate, a Fourier transform, and a streaming
// short-time spectrum. Standard library only; nothing here allocates per
// sample.
package dsp

import (
	"errors"
	"math"
	"math/cmplx"

	"github.com/gabeduke/hindsight/internal/audio"
)

// FFT transforms x in place. len(x) must be a power of two.
func FFT(x []complex128) error {
	n := len(x)
	if n == 0 || n&(n-1) != 0 {
		return errors.New("dsp: FFT length must be a power of two")
	}
	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		half := size >> 1
		for start := 0; start < n; start += size {
			t := complex(1, 0)
			for k := 0; k < half; k++ {
				a, b := x[start+k], x[start+k+half]*t
				x[start+k], x[start+k+half] = a+b, a-b
				t *= w
			}
		}
	}
	return nil
}

// Hann is the n-point periodic Hann window.
func Hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}

// MonoRate is the rate a take is read at for analysis: the take's own rate
// divided by the whole number that brings it nearest 12 kHz. Everything an
// analyser wants -- pitch, timbre, onsets -- is under 6 kHz, and a quarter of
// the samples is a quarter of the work on a Pi.
func MonoRate(sampleRate int) (rate, factor int) {
	factor = int(math.Round(float64(sampleRate) / 12000))
	if factor < 1 {
		factor = 1
	}
	return sampleRate / factor, factor
}

// ReadMono streams a take as float32 mono at MonoRate, in blocks, to fn: its
// channels averaged, then every factor samples averaged into one (a boxcar
// low-pass, which is crude but is what keeps a 14 kHz hat from folding into
// the notes). The block passed to fn is reused. It returns the take's header
// and the rate the blocks are at.
func ReadMono(path string, fn func(block []float32) error) (audio.WAVInfo, int, error) {
	info, err := audio.ReadWAVInfo(path)
	if err != nil {
		return info, 0, err
	}
	if info.Frames() == 0 {
		return info, 0, audio.ErrRange
	}
	rate, factor := MonoRate(info.SampleRate)
	ch := info.Channels
	const scale = 1.0 / 2147483648.0
	var out []float32
	var acc float64
	var n int
	_, err = audio.ReadFrames(path, 0, info.Frames(), 1<<14, func(block []int32, _ int64) error {
		out = out[:0]
		for i := 0; i+ch <= len(block); i += ch {
			var s float64
			for c := 0; c < ch; c++ {
				s += float64(block[i+c])
			}
			acc += s * scale / float64(ch)
			n++
			if n == factor {
				out = append(out, float32(acc/float64(factor)))
				acc, n = 0, 0
			}
		}
		if len(out) == 0 {
			return nil
		}
		return fn(out)
	})
	return info, rate, err
}

// STFT turns a stream of samples into a stream of magnitude spectra: frames
// of Size samples every Hop, Hann-windowed. Push it blocks of any length.
type STFT struct {
	Size, Hop int

	win  []float64
	buf  []float64
	have int
	x    []complex128
	mag  []float64
	t    int64 // samples consumed before buf[0]
}

// NewSTFT makes one. size must be a power of two and hop in (0, size].
func NewSTFT(size, hop int) (*STFT, error) {
	if size <= 0 || size&(size-1) != 0 || hop <= 0 || hop > size {
		return nil, errors.New("dsp: STFT needs a power-of-two size and 0 < hop <= size")
	}
	return &STFT{Size: size, Hop: hop, win: Hann(size), buf: make([]float64, size),
		x: make([]complex128, size), mag: make([]float64, size/2+1)}, nil
}

// Push feeds samples and calls fn for every frame they complete, with the
// magnitude spectrum (bins 0..Size/2; reused) and the frame's first sample's
// index in the stream.
func (s *STFT) Push(samples []float32, fn func(mag []float64, firstSample int64)) {
	for _, v := range samples {
		s.buf[s.have] = float64(v)
		s.have++
		if s.have < s.Size {
			continue
		}
		for i := range s.x {
			s.x[i] = complex(s.buf[i]*s.win[i], 0)
		}
		_ = FFT(s.x) // size is a power of two: checked in NewSTFT
		for i := range s.mag {
			s.mag[i] = cmplx.Abs(s.x[i])
		}
		fn(s.mag, s.t)
		copy(s.buf, s.buf[s.Hop:])
		s.have = s.Size - s.Hop
		s.t += int64(s.Hop)
	}
}

// RMS is the root-mean-square of x, 0 for none.
func RMS(x []float32) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}

// DB is 20·log10 of an amplitude, floored at -120 so silence is a number.
func DB(a float64) float64 {
	if a < 1e-6 {
		return -120
	}
	return 20 * math.Log10(a)
}
