// Package produce turns a take's stems (or several takes) into a finished
// song: leveled against each other, mixed, brought to a loudness target with
// headroom, faded, and written as a file to share. It is the "script to
// produce a song" for post production, and it drives ffmpeg, as renders do
// (internal/audio/render.go).
//
// The chain, each step a decision the Plan makes and not the code:
//
//  1. Measure every stem (EBU R128 integrated loudness, ffmpeg's loudnorm in
//     measure mode) and bring each to the same level, so a quiet bass and a
//     hot guitar start even. The change is capped (MaxStemGainDB) so a stem
//     of room tone is not boosted into a hiss.
//  2. High-pass each stem (default 30 Hz): rumble and handling noise cost
//     headroom and say nothing.
//  3. Sum them (amix with normalisation off: the levels are already set).
//  4. Measure the mix and bring it to the Target loudness with a true-peak
//     ceiling, in loudnorm's two-pass linear mode, which moves the level
//     without squashing the dynamics the way its single pass does.
//  5. Fade in and out, and write 24-bit WAV, FLAC or MP3 by the file's name.
//
// Everything that decides ffmpeg's arguments is a pure function of the Plan
// (Args, Filter) and everything that reads ffmpeg's answer is a pure parser
// (ParseLoudness), so the tests need ffmpeg only for one end-to-end check.
package produce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabeduke/hindsight/internal/audio"
)

// Plan is what to make.
type Plan struct {
	Inputs []string // WAV files: the stems, or the takes, to mix
	Output string   // .wav (24-bit), .flac or .mp3

	TargetLUFS  float64 // the mix's integrated loudness; -14 if 0
	TruePeakDB  float64 // the ceiling; -1 if 0
	StemLUFS    float64 // each stem is brought to this first; -23 if 0
	HighpassHz  float64 // 30 if 0; negative turns it off
	FadeIn      float64 // seconds
	FadeOut     float64 // seconds
	MaxStemGain float64 // dB a stem may be moved either way; 18 if 0
	// Gains is an extra per-stem trim in dB, by input index, applied after
	// leveling: "the bass sits a little under".
	Gains map[int]float64
}

// Loudness is what ffmpeg's loudnorm measured.
type Loudness struct {
	Integrated float64 // LUFS
	TruePeak   float64 // dBTP
	Range      float64 // LU
	Threshold  float64
	Offset     float64
}

func (p *Plan) defaults() {
	if p.TargetLUFS == 0 {
		p.TargetLUFS = -14
	}
	if p.TruePeakDB == 0 {
		p.TruePeakDB = -1
	}
	if p.StemLUFS == 0 {
		p.StemLUFS = -23
	}
	if p.HighpassHz == 0 {
		p.HighpassHz = 30
	}
	if p.MaxStemGain == 0 {
		p.MaxStemGain = 18
	}
}

// Validate says what is wrong with the Plan, ready to show.
func (p Plan) Validate() error {
	p.defaults()
	if len(p.Inputs) == 0 {
		return errors.New("produce: nothing to mix")
	}
	switch strings.ToLower(filepath.Ext(p.Output)) {
	case ".wav", ".flac", ".mp3":
	default:
		return fmt.Errorf("produce: the output must be .wav, .flac or .mp3, not %q", filepath.Ext(p.Output))
	}
	for _, in := range p.Inputs {
		if in == p.Output {
			return errors.New("produce: the output is also an input")
		}
	}
	if p.TargetLUFS > -5 || p.TargetLUFS < -40 {
		return fmt.Errorf("produce: target %.1f LUFS is out of range (-40 to -5)", p.TargetLUFS)
	}
	if p.TruePeakDB > 0 {
		return errors.New("produce: the true-peak ceiling must be 0 dB or lower")
	}
	if p.FadeIn < 0 || p.FadeOut < 0 {
		return errors.New("produce: a fade cannot be negative")
	}
	return nil
}

// stemFilter is one stem's chain: high-pass, then its gain.
func (p Plan) stemFilter(i int, measured Loudness) string {
	gain := p.StemLUFS - measured.Integrated
	if math.IsInf(measured.Integrated, 0) || math.IsNaN(measured.Integrated) || measured.Integrated < -69 {
		gain = 0 // silence: leave it alone
	}
	gain = math.Max(-p.MaxStemGain, math.Min(p.MaxStemGain, gain)) + p.Gains[i]
	var parts []string
	if p.HighpassHz > 0 {
		parts = append(parts, fmt.Sprintf("highpass=f=%s", num(p.HighpassHz)))
	}
	parts = append(parts, fmt.Sprintf("volume=%sdB", num(gain)))
	return fmt.Sprintf("[%d:a]%s[s%d]", i, strings.Join(parts, ","), i)
}

// Mix is the filter graph up to and including the sum, labelled [mix].
func (p Plan) Mix(stems []Loudness) string {
	p.defaults()
	var f []string
	var labels string
	for i := range p.Inputs {
		f = append(f, p.stemFilter(i, stems[i]))
		labels += fmt.Sprintf("[s%d]", i)
	}
	if len(p.Inputs) == 1 {
		f = append(f, "[s0]anull[mix]")
	} else {
		f = append(f, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=longest[mix]", labels, len(p.Inputs)))
	}
	return strings.Join(f, ";")
}

// Master is the filter after the sum. With `mix` nil it measures (loudnorm
// prints JSON on stderr); with the mix's measurement it applies the gain
// linearly, then the fades, then a limiter at the ceiling as a safety net.
func (p Plan) Master(mix *Loudness, duration float64) string {
	p.defaults()
	ln := fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=11", num(p.TargetLUFS), num(p.TruePeakDB))
	if mix == nil {
		return ln + ":print_format=json"
	}
	ln += fmt.Sprintf(":measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true",
		num(mix.Integrated), num(mix.TruePeak), num(mix.Range), num(mix.Threshold), num(mix.Offset))
	parts := []string{ln}
	if p.FadeIn > 0 {
		parts = append(parts, fmt.Sprintf("afade=t=in:d=%s", num(p.FadeIn)))
	}
	if p.FadeOut > 0 && duration > p.FadeOut {
		parts = append(parts, fmt.Sprintf("afade=t=out:st=%s:d=%s", num(duration-p.FadeOut), num(p.FadeOut)))
	}
	// loudnorm's own limit is inter-sample-peak aware; this guards the
	// fades' and the resampler's last fraction of a dB.
	parts = append(parts, fmt.Sprintf("alimiter=limit=%s:level=disabled", num(math.Pow(10, p.TruePeakDB/20))))
	return strings.Join(parts, ",")
}

// codecArgs is the encoder's arguments for the output's extension.
func codecArgs(output string) []string {
	switch strings.ToLower(filepath.Ext(output)) {
	case ".flac":
		return []string{"-c:a", "flac", "-sample_fmt", "s32"}
	case ".mp3":
		return []string{"-c:a", "libmp3lame", "-b:a", "256k"}
	default:
		return []string{"-c:a", "pcm_s24le"}
	}
}

func inputArgs(inputs []string) []string {
	a := []string{"-hide_banner", "-nostdin"}
	for _, in := range inputs {
		a = append(a, "-i", in)
	}
	return a
}

// MeasureArgs measures one file's loudness.
func MeasureArgs(path string) []string {
	return []string{"-hide_banner", "-nostdin", "-i", path, "-af", "loudnorm=print_format=json", "-f", "null", "-"}
}

// MeasureMixArgs runs the mix through the measuring master, to nowhere.
func (p Plan) MeasureMixArgs(stems []Loudness) []string {
	a := inputArgs(p.Inputs)
	return append(a, "-filter_complex", p.Mix(stems)+";[mix]"+p.Master(nil, 0)+"[out]", "-map", "[out]", "-f", "null", "-")
}

// Args is the final render's ffmpeg arguments.
func (p Plan) Args(stems []Loudness, mix Loudness, duration float64) []string {
	a := inputArgs(p.Inputs)
	a = append(a, "-filter_complex", p.Mix(stems)+";[mix]"+p.Master(&mix, duration)+"[out]", "-map", "[out]", "-ar", "48000")
	a = append(a, codecArgs(p.Output)...)
	return append(a, "-y", p.Output)
}

// ParseLoudness reads the JSON block loudnorm prints at the end of a run.
func ParseLoudness(stderr string) (Loudness, error) {
	i := strings.LastIndex(stderr, "{")
	j := strings.LastIndex(stderr, "}")
	if i < 0 || j < i {
		return Loudness{}, errors.New("produce: ffmpeg gave no loudness measurement")
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(stderr[i:j+1]), &raw); err != nil {
		return Loudness{}, fmt.Errorf("produce: unreadable loudness measurement: %w", err)
	}
	get := func(k string) (float64, error) {
		v, ok := raw[k]
		if !ok {
			return 0, fmt.Errorf("produce: loudness measurement has no %s", k)
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("produce: loudness %s %q is not a number", k, v)
		}
		return f, nil
	}
	var l Loudness
	var err error
	for _, f := range []struct {
		key string
		to  *float64
	}{{"input_i", &l.Integrated}, {"input_tp", &l.TruePeak}, {"input_lra", &l.Range}, {"input_thresh", &l.Threshold}, {"target_offset", &l.Offset}} {
		if *f.to, err = get(f.key); err != nil {
			return Loudness{}, err
		}
	}
	return l, nil
}

// Runner runs ffmpeg with args and returns what it wrote to stderr.
type Runner func(ctx context.Context, args []string) (stderr string, err error)

// FFmpeg is the Runner that runs the real ffmpeg, at nice 10 so it does not
// compete with a rig that is capturing.
func FFmpeg(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "nice", append([]string{"-n", "10", "ffmpeg"}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stderr.String(), fmt.Errorf("ffmpeg: %w: %s", err, tail(stderr.String()))
	}
	return stderr.String(), nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = "…" + s[len(s)-400:]
	}
	return s
}

// Report is what a run did.
type Report struct {
	Stems    []Loudness `json:"stems"`
	Mix      Loudness   `json:"mix"`
	Duration float64    `json:"duration_seconds"`
	Output   string     `json:"output"`
}

// Run makes the song. progress, if set, is told each step in words.
func Run(ctx context.Context, p Plan, run Runner, progress func(string)) (Report, error) {
	if err := p.Validate(); err != nil {
		return Report{}, err
	}
	p.defaults()
	say := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	var dur float64
	for _, in := range p.Inputs {
		info, err := audio.ReadWAVInfo(in)
		if err != nil {
			return Report{}, fmt.Errorf("produce: %s: %w", filepath.Base(in), err)
		}
		if d := float64(info.Frames()) / float64(info.SampleRate); d > dur {
			dur = d
		}
	}
	stems := make([]Loudness, len(p.Inputs))
	for i, in := range p.Inputs {
		say(fmt.Sprintf("Measuring %s", filepath.Base(in)))
		out, err := run(ctx, MeasureArgs(in))
		if err != nil {
			return Report{}, err
		}
		if stems[i], err = ParseLoudness(out); err != nil {
			return Report{}, fmt.Errorf("%s: %w", filepath.Base(in), err)
		}
	}
	say("Measuring the mix")
	out, err := run(ctx, p.MeasureMixArgs(stems))
	if err != nil {
		return Report{}, err
	}
	mix, err := ParseLoudness(out)
	if err != nil {
		return Report{}, fmt.Errorf("the mix: %w", err)
	}
	say("Rendering " + filepath.Base(p.Output))
	if _, err := run(ctx, p.Args(stems, mix, dur)); err != nil {
		return Report{}, err
	}
	return Report{Stems: stems, Mix: mix, Duration: dur, Output: p.Output}, nil
}

// num writes a number for a filter: no exponent, no trailing zeros.
func num(f float64) string {
	return strconv.FormatFloat(math.Round(f*1000)/1000, 'f', -1, 64)
}
