package instrument

import (
	"math"
	"sort"
)

// The labels a take can be given. They are what a take sounds like, not what
// was plugged in: a guitar played as chords is "chords", and the same guitar
// playing a line is "lead". Telling a guitar from a keyboard needs a trained
// model; this is the part that is worth having before one.
const (
	Percussion = "percussion"
	Bass       = "bass"
	Chords     = "chords"
	Lead       = "lead"
	Ambience   = "ambience"
	Silence    = "silence"
)

// Guess is one label and how sure the classifier is, 0 to 1.
type Guess struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// Classifier turns a take's Features into guesses, best first. It is the
// seam a trained model goes behind: it gets the same Features, and the
// Analyzer stores whatever Name it reports with the guesses.
type Classifier interface {
	Name() string
	Classify(Features) []Guess
}

// Rules is the classifier that ships: hand-set thresholds over Features,
// smoothed so that a take can be two things at once (a kit with a bass line).
// It is a proof of concept. The thresholds were set against synthetic
// instruments and need tuning on real takes, which is what keeping the
// Features beside each verdict is for.
type Rules struct{}

func (Rules) Name() string { return "rules-1" }

// step is a smooth 0..1 rise through the edge: 0.5 at edge, and over about
// width either side.
func step(x, edge, width float64) float64 {
	return 1 / (1 + math.Exp(-(x-edge)/width*4))
}

func (Rules) Classify(f Features) []Guess {
	if f.PeakDB < -60 || f.Active < 0.02 {
		return []Guess{{Silence, 1}}
	}
	// Hits that die away between beats: a short sustain and onsets that
	// come often, or a noisy spectrum.
	hits := step(f.OnsetRate, 1.2, 1.0) * step(-f.Sustain, -0.8, 0.1)
	noisy := step(f.Flatness, 0.12, 0.08)
	// A kit that sits under something else shows in the top of the spectrum:
	// separated hits there, and enough energy there to count.
	kit := step(f.PercussiveHigh, 0.65, 0.1) * step(f.HighShare, 0.0002, 0.0002)
	percussion := math.Max(math.Max(hits, noisy*step(f.OnsetRate, 0.5, 0.5)), kit)

	pitched := 1 - noisy
	held := step(f.Sustain, 0.8, 0.1)

	bass := step(f.LowRatio, 0.5, 0.15) * step(-f.Centroid, -600, 250) * pitched
	chords := step(f.Peaks, 5, 1.5) * pitched * (1 - bass*0.5)
	lead := step(-f.Peaks, -5, 1.5) * pitched * step(f.Centroid, 350, 150) * (1 - bass)
	ambience := noisy * held * step(-f.OnsetRate, -0.5, 0.5)
	// A noisy sustained wash is not percussion.
	percussion *= 1 - ambience

	all := []Guess{
		{Percussion, percussion}, {Bass, bass}, {Chords, chords}, {Lead, lead}, {Ambience, ambience},
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	var out []Guess
	for _, g := range all {
		if g.Score >= 0.5 && len(out) < 3 {
			g.Score = math.Round(g.Score*100) / 100
			out = append(out, g)
		}
	}
	return out
}
