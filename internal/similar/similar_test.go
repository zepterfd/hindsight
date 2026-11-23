package similar

import (
	"context"
	"math"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
)

const sr = 48000

// A progression is a list of chords: root pitch class and minor or not.
type chord struct {
	root  int
	minor bool
}

func hz(pc, octave int) float64 { return 440 * math.Pow(2, float64(pc-9+12*(octave-4))/12) }

// render plays a progression `cycles` times, each chord `secs` long, in the
// given transposition and with a number of harmonics (its timbre).
func render(prog []chord, transpose int, secs float64, cycles, harmonics int) []float64 {
	n := int(secs * sr)
	out := make([]float64, 0, n*len(prog)*cycles)
	for c := 0; c < cycles; c++ {
		for _, ch := range prog {
			root := (ch.root + transpose) % 12
			third := 4
			if ch.minor {
				third = 3
			}
			notes := []float64{hz(root, 3), hz(root+third, 3), hz(root+7, 3)}
			buf := make([]float64, n)
			for _, f := range notes {
				for i := range buf {
					t := float64(i) / sr
					env := math.Min(1, t/0.02) * math.Min(1, (secs-t)/0.05)
					for h := 1; h <= harmonics; h++ {
						buf[i] += 0.15 * env * math.Sin(2*math.Pi*f*float64(h)*t) / float64(h)
					}
				}
			}
			out = append(out, buf...)
		}
	}
	return out
}

func writeTake(t *testing.T, dir, name string, sig []float64) string {
	t.Helper()
	data := make([]int32, len(sig)*2)
	for i, v := range sig {
		s := int32(v * math.MaxInt32)
		data[2*i], data[2*i+1] = s, s
	}
	p := filepath.Join(dir, name)
	if _, err := audio.WriteWAV(p, data, 2, []int{0, 1}, sr); err != nil {
		t.Fatal(err)
	}
	return p
}

func fingerprint(t *testing.T, sig []float64) Print {
	t.Helper()
	p := writeTake(t, t.TempDir(), "x.wav", sig)
	out, err := Analyzer{}.Analyze(context.Background(), analyze.Input{Path: p, Info: mustInfo(t, p)})
	if err != nil {
		t.Fatal(err)
	}
	return out.(Print)
}

func mustInfo(t *testing.T, p string) audio.WAVInfo {
	i, err := audio.ReadWAVInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

var (
	// I V vi IV in C.
	pop = []chord{{0, false}, {7, false}, {9, true}, {5, false}}
	// ii V I vi in C: the same chords' colours, a different journey.
	jazz = []chord{{2, true}, {7, false}, {0, false}, {9, true}}
	// i VII VI VII in A minor.
	rock = []chord{{9, true}, {7, false}, {5, false}, {7, false}}
)

func TestChordsAreNamedInOrder(t *testing.T) {
	p := fingerprint(t, render(pop, 0, 1.5, 2, 6))
	var got []string
	for _, s := range p.Chords {
		got = append(got, s.Chord)
	}
	want := []string{"C", "G", "Am", "F", "C", "G", "Am", "F"}
	if len(got) != len(want) {
		t.Fatalf("chords = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chords = %v, want %v", got, want)
		}
	}
	if p.Key != "C" {
		t.Errorf("key = %q, want C", p.Key)
	}
	if p.Changes != 7 {
		t.Errorf("changes = %d, want 7", p.Changes)
	}
}

func TestTheSameProgressionInAnotherKeyAndTimbreIsAlike(t *testing.T) {
	inC := fingerprint(t, render(pop, 0, 1.5, 3, 6))
	inG := fingerprint(t, render(pop, 7, 1.2, 4, 3)) // a fifth up, faster, duller
	jz := fingerprint(t, render(jazz, 0, 1.5, 3, 6))
	rk := fingerprint(t, render(rock, 0, 1.5, 3, 6))

	same := Score(inC, inG)
	if same < 0.9 {
		t.Errorf("pop in C vs pop in G = %.3f, want >= 0.9", same)
	}
	for name, other := range map[string]Print{"jazz": jz, "rock": rk} {
		if s := Score(inC, other); s >= same-0.1 {
			t.Errorf("pop vs %s = %.3f, not clearly below pop vs pop transposed %.3f", name, s, same)
		}
	}
	if Score(inC, inG) != Score(inG, inC) {
		t.Error("Score is not symmetric")
	}
}

func TestGroupsFindThePopTakesAndLeaveTheRest(t *testing.T) {
	es := []Entry{
		{"a_pop_c.wav", fingerprint(t, render(pop, 0, 1.5, 3, 6))},
		{"b_jazz.wav", fingerprint(t, render(jazz, 0, 1.5, 3, 6))},
		{"c_pop_g.wav", fingerprint(t, render(pop, 7, 1.2, 4, 3))},
		{"d_rock.wav", fingerprint(t, render(rock, 0, 1.5, 3, 6))},
		{"e_pop_eb.wav", fingerprint(t, render(pop, 3, 1.5, 3, 4))},
	}
	gs := Groups(es, 0.85)
	if len(gs) != 1 {
		t.Fatalf("groups = %+v, want exactly the pop takes", gs)
	}
	want := []string{"a_pop_c.wav", "c_pop_g.wav", "e_pop_eb.wav"}
	if len(gs[0].Members) != 3 {
		t.Fatalf("members = %v", gs[0].Members)
	}
	for i := range want {
		if gs[0].Members[i] != want[i] {
			t.Errorf("members = %v, want %v", gs[0].Members, want)
		}
	}
	if gs[0].Score < 0.85 {
		t.Errorf("group score = %v; it is the weakest pairing and must be >= the threshold", gs[0].Score)
	}

	// Rank puts the other pop takes first.
	r := Rank(es[0], es, 0, 0)
	if len(r) != 4 || (r[0].Name != "c_pop_g.wav" && r[0].Name != "e_pop_eb.wav") {
		t.Errorf("rank = %+v", r)
	}
	if r[len(r)-1].Name == "c_pop_g.wav" {
		t.Errorf("rank = %+v", r)
	}
}

func TestACompleteLinkageDoesNotChain(t *testing.T) {
	// a~b and b~c but not a~c: they are not one group.
	mk := func(name string, moves [48]float64) Entry {
		p := Print{Changes: 10, Moves: moves}
		p.Profile[0] = 1
		return Entry{name, p}
	}
	var m1, m2, m3 [48]float64
	m1[0], m1[1] = 1, 0
	m2[0], m2[1] = 1, 1
	m3[1] = 1
	gs := Groups([]Entry{mk("a", m1), mk("b", m2), mk("c", m3)}, 0.85)
	for _, g := range gs {
		if len(g.Members) == 3 {
			t.Errorf("chained a, b and c into one group: %+v", g)
		}
	}
}

func TestADroneAndNoiseMatchNothing(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	noise := make([]float64, 6*sr)
	for i := range noise {
		noise[i] = 0.2 * (rng.Float64()*2 - 1)
	}
	n := fingerprint(t, noise)
	one := fingerprint(t, render([]chord{{0, false}}, 0, 8, 1, 6))
	pp := fingerprint(t, render(pop, 0, 1.5, 3, 6))
	if n.Changes >= MinChanges && Score(n, pp) > 0.5 {
		t.Errorf("noise scored %.2f against a progression (%d changes)", Score(n, pp), n.Changes)
	}
	if one.Changes != 0 || Score(one, one) != 0 || Score(one, pp) != 0 {
		t.Errorf("a single chord is not a progression: %+v", one.Changes)
	}
	if gs := Groups([]Entry{{"a", one}, {"b", one}}, 0.5); len(gs) != 0 {
		t.Errorf("grouped two drones: %+v", gs)
	}
}

func TestStoredFingerprintIsBounded(t *testing.T) {
	var b builder
	b = *newBuilder(12000)
	for i := 0; i < 3000; i++ {
		b.energies = append(b.energies, 1)
		b.ids = append(b.ids, (i/4)%2*7) // alternate C and G every 4 frames
	}
	p := b.print(1000)
	if len(p.Chords) != maxChords {
		t.Errorf("chords = %d, want the cap %d", len(p.Chords), maxChords)
	}
	if p.Changes < 600 {
		t.Errorf("changes = %d; Moves must be made from all of them, not the capped list", p.Changes)
	}
}

func TestACancelledReadStops(t *testing.T) {
	p := writeTake(t, t.TempDir(), "x.wav", render(pop, 0, 1.5, 1, 4))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Analyzer{}).Analyze(ctx, analyze.Input{Path: p, Info: mustInfo(t, p)}); err == nil {
		t.Error("a cancelled fingerprint returned")
	}
}

func TestKeyAndVersion(t *testing.T) {
	var a analyze.Analyzer = Analyzer{}
	if a.Key() != "harmony" || a.Version() < 1 {
		t.Error(a.Key())
	}
}
