package similar

import (
	"math"
	"sort"
)

// MinChanges is how many chord changes a fingerprint needs before it says
// anything about a progression.
const MinChanges = 3

// Score says how alike two takes' harmony is, 0 to 1. It is half the
// progression (the Moves, which do not depend on the key) and half the colour
// of the take (the Profile, compared in whichever of the twelve transpositions
// fits best, so a take in G and the same take in C match). A take with
// fewer than MinChanges changes scores 0 against everything.
func Score(a, b Print) float64 {
	if a.Changes < MinChanges || b.Changes < MinChanges {
		return 0
	}
	var moves float64
	for i := range a.Moves {
		moves += a.Moves[i] * b.Moves[i]
	}
	// Unit-length them, as Moves are shares and not a vector.
	moves /= math.Sqrt(sumSq(a.Moves[:]) * sumSq(b.Moves[:]))
	var best float64
	for r := 0; r < 12; r++ {
		var d float64
		for i := 0; i < 12; i++ {
			d += a.Profile[i] * b.Profile[(i+r)%12]
		}
		best = math.Max(best, d)
	}
	return 0.5*moves + 0.5*best
}

func sumSq(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x * x
	}
	return s
}

// Entry is a take and its fingerprint.
type Entry struct {
	Name  string
	Print Print
}

// Match is another take and how like it is.
type Match struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Key   string  `json:"key"`
}

// Rank orders the others by how like target they are, best first, dropping
// any below min, at most limit.
func Rank(target Entry, others []Entry, min float64, limit int) []Match {
	var out []Match
	for _, o := range others {
		if o.Name == target.Name {
			continue
		}
		if s := Score(target.Print, o.Print); s >= min {
			out = append(out, Match{o.Name, round(s), o.Print.Key})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func round(x float64) float64 { return math.Round(x*1000) / 1000 }

// Group is takes that play alike.
type Group struct {
	// Members are the take names, in the order they were given.
	Members []string `json:"members"`
	// Score is the group's weakest pairing, which is what a person wants
	// to know: that every take in it is at least this like every other.
	Score float64 `json:"score"`
}

// Groups clusters the entries by complete linkage: a take joins a group only
// if it is at least min alike to *every* take already in it, so a chain of
// takes each a little different from the last is not one group. Takes with no
// progression are left out. Groups of one are dropped. Given the same entries
// in the same order it gives the same groups.
func Groups(entries []Entry, min float64) []Group {
	type cluster struct{ idx []int }
	var cs []*cluster
	for i, e := range entries {
		if e.Print.Changes < MinChanges {
			continue
		}
		cs = append(cs, &cluster{[]int{i}})
	}
	link := func(a, b *cluster) float64 {
		worst := 1.0
		for _, i := range a.idx {
			for _, j := range b.idx {
				worst = math.Min(worst, Score(entries[i].Print, entries[j].Print))
			}
		}
		return worst
	}
	for {
		bi, bj, best := -1, -1, min
		for i := 0; i < len(cs); i++ {
			for j := i + 1; j < len(cs); j++ {
				if l := link(cs[i], cs[j]); l >= best && (bi < 0 || l > best) {
					bi, bj, best = i, j, l
				}
			}
		}
		if bi < 0 {
			break
		}
		cs[bi].idx = append(cs[bi].idx, cs[bj].idx...)
		cs = append(cs[:bj], cs[bj+1:]...)
	}
	var out []Group
	for _, c := range cs {
		if len(c.idx) < 2 {
			continue
		}
		sort.Ints(c.idx)
		g := Group{Score: 1}
		for _, i := range c.idx {
			g.Members = append(g.Members, entries[i].Name)
		}
		for a := 0; a < len(c.idx); a++ {
			for b := a + 1; b < len(c.idx); b++ {
				g.Score = math.Min(g.Score, Score(entries[c.idx[a]].Print, entries[c.idx[b]].Print))
			}
		}
		g.Score = round(g.Score)
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return out[i].Members[0] < out[j].Members[0]
	})
	return out
}
