// Command hindsight-produce mixes stems into a finished song: leveled,
// summed, brought to a loudness target with headroom, faded, and written as
// WAV, FLAC or MP3 (internal/produce).
//
//	hindsight-produce -o song.flac bass.wav drums.wav guitar.wav
//	hindsight-produce -o song.mp3 -lufs -16 -fade-out 4 -trim 1=-2 take_*.wav
//
// It needs ffmpeg on the PATH. Nothing is read from, or written to, the
// Pi's takes folder unless you name it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/gabeduke/hindsight/internal/produce"
)

// trims is -trim i=dB, repeatable: a stem's own gain after leveling.
type trims map[int]float64

func (t trims) String() string { return fmt.Sprint(map[int]float64(t)) }
func (t trims) Set(v string) error {
	k, g, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("want INDEX=DB, got %q", v)
	}
	i, err := strconv.Atoi(k)
	if err != nil || i < 0 {
		return fmt.Errorf("bad stem index %q", k)
	}
	db, err := strconv.ParseFloat(g, 64)
	if err != nil {
		return fmt.Errorf("bad gain %q", g)
	}
	t[i] = db
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("hindsight-produce", flag.ContinueOnError)
	fs.SetOutput(stderr)
	p := produce.Plan{Gains: trims{}}
	fs.StringVar(&p.Output, "o", "", "output file (.wav, .flac or .mp3)")
	fs.Float64Var(&p.TargetLUFS, "lufs", -14, "target integrated loudness of the mix")
	fs.Float64Var(&p.TruePeakDB, "tp", -1, "true-peak ceiling in dB")
	fs.Float64Var(&p.StemLUFS, "stem-lufs", -23, "level each stem is brought to before the mix")
	fs.Float64Var(&p.HighpassHz, "highpass", 30, "high-pass each stem at this many Hz (negative: off)")
	fs.Float64Var(&p.FadeIn, "fade-in", 0, "seconds")
	fs.Float64Var(&p.FadeOut, "fade-out", 0, "seconds")
	fs.Var(trims(p.Gains), "trim", "INDEX=DB: a stem's own gain after leveling (repeatable)")
	asJSON := fs.Bool("json", false, "print the measurements as JSON when done")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p.Inputs = fs.Args()
	if err := p.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, err := produce.Run(ctx, p, produce.FFmpeg, func(s string) { fmt.Fprintln(stderr, s) })
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(rep)
	} else {
		fmt.Fprintf(stdout, "%s: %.0f s, mix was %.1f LUFS (true peak %.1f dB), now %s LUFS\n",
			rep.Output, rep.Duration, rep.Mix.Integrated, rep.Mix.TruePeak, strconv.FormatFloat(p.TargetLUFS, 'f', -1, 64))
	}
	return 0
}
