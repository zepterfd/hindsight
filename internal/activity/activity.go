// Package activity turns the list of takes into a feed: what happened in the
// room lately, grouped the way a person remembers it (a session, a theme, "this
// day last year"), with a few suggested next steps. It is the Listening Room's
// answer to a photo library's "On this Day" and recent-activity cards.
//
// It is pure: takes and a clock in, cards out, no files read and no state kept,
// so the feed is as cheap as the list it is made from (audio.TakeList caches
// that) and every rule here is a unit test. Which cards the page draws, and how,
// is the page's; the server only says what is true.
package activity

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

// Kind says what a card is, for a page to choose how to draw it.
type Kind string

const (
	OnThisDay  Kind = "on_this_day" // takes from this date in an earlier year
	Session    Kind = "session"     // takes made in one sitting
	Theme      Kind = "theme"       // takes that share a tag or a tempo
	Suggestion Kind = "suggestion"  // something worth doing to some takes
)

// Card is one thing in the feed.
type Card struct {
	// ID is stable across requests while the card's meaning is, so a page can
	// keep a card dismissed or open.
	ID       string  `json:"id"`
	Kind     Kind    `json:"kind"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Count    int     `json:"count"`
	Seconds  float64 `json:"seconds"`
	// Takes are the takes' filenames, newest first, at most Options.PerCard of
	// them; Count says how many there are in all.
	Takes []string `json:"takes"`
	// Action, on a Suggestion, is what to do to them: "label", "star" or
	// "trim_silence".
	Action string `json:"action,omitempty"`
	// Tag, on a Theme of a tag, is the tag's id; the page knows its name and
	// colour. BPM, on a Theme of a tempo, is its middle.
	Tag string  `json:"tag,omitempty"`
	BPM float64 `json:"bpm,omitempty"`
	// At is the card's newest take, for ordering and for a page's "2 days ago".
	At time.Time `json:"at"`
}

// Day is one day's activity, for the strip of activity over time.
type Day struct {
	Date    string  `json:"date"` // 2026-11-23, in the feed's zone
	Count   int     `json:"count"`
	Seconds float64 `json:"seconds"`
}

// Feed is what GET /api/activity answers.
type Feed struct {
	Now   time.Time `json:"now"`
	Zone  string    `json:"zone"`
	Cards []Card    `json:"cards"`
	Days  []Day     `json:"days"`
}

// Options are the feed's rules. The zero value is what the page uses.
type Options struct {
	// PerCard caps the takes a card lists. 6 by default.
	PerCard int
	// SessionGap is the quiet that ends a session. 2 hours by default.
	SessionGap time.Duration
	// SessionDays is how far back sessions are drawn. 14 by default.
	SessionDays int
	// ThemeDays is how far back themes look. 60 by default.
	ThemeDays int
	// ThemeMin is the fewest takes that make a theme. 3 by default.
	ThemeMin int
	// OnThisDayYears is how many years back to look. 10 by default.
	OnThisDayYears int
	// LongTake is the length, in seconds, past which an unselected take is
	// worth trimming. 300 by default.
	LongTake float64
	// Settle is how old a take must be before it is suggested for naming: a
	// take just kept is still being looked at. 1 hour by default.
	Settle time.Duration
	// Days is the length of the activity strip. 60 by default.
	Days int
}

func (o Options) withDefaults() Options {
	if o.PerCard <= 0 {
		o.PerCard = 6
	}
	if o.SessionGap <= 0 {
		o.SessionGap = 2 * time.Hour
	}
	if o.SessionDays <= 0 {
		o.SessionDays = 14
	}
	if o.ThemeDays <= 0 {
		o.ThemeDays = 60
	}
	if o.ThemeMin <= 0 {
		o.ThemeMin = 3
	}
	if o.OnThisDayYears <= 0 {
		o.OnThisDayYears = 10
	}
	if o.LongTake <= 0 {
		o.LongTake = 300
	}
	if o.Settle <= 0 {
		o.Settle = time.Hour
	}
	if o.Days <= 0 {
		o.Days = 60
	}
	return o
}

// Build makes the feed for `now`, in zone `loc` (a take made at 11 pm is of
// that day for the person who made it). takes may be in any order and may hold
// cuts and mixes: a cut is a take made from one, and counts as its own work.
func Build(takes []audio.Take, now time.Time, loc *time.Location, opt Options) Feed {
	opt = opt.withDefaults()
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	own := make([]audio.Take, 0, len(takes))
	for _, t := range takes {
		if t.Created.IsZero() || t.Created.After(now.Add(time.Minute)) {
			continue // a take from the future has a wrong clock; it has no place in a feed
		}
		own = append(own, t)
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].Created.After(own[j].Created) })

	f := Feed{Now: now, Zone: loc.String(), Cards: []Card{}, Days: days(own, now, loc, opt.Days)}
	f.Cards = append(f.Cards, onThisDay(own, now, loc, opt)...)
	f.Cards = append(f.Cards, sessions(own, now, loc, opt)...)
	f.Cards = append(f.Cards, themes(own, now, loc, opt)...)
	f.Cards = append(f.Cards, suggestions(own, now, opt)...)
	return f
}

func newCard(id string, kind Kind, title, sub string, ts []audio.Take, perCard int) Card {
	c := Card{ID: id, Kind: kind, Title: title, Subtitle: sub, Count: len(ts), Takes: []string{}}
	for i, t := range ts { // ts is newest first
		c.Seconds += t.Duration
		if i == 0 {
			c.At = t.Created
		}
		if i < perCard {
			c.Takes = append(c.Takes, t.Name)
		}
	}
	c.Seconds = math.Round(c.Seconds*10) / 10
	return c
}

func dateKey(t time.Time) string { return t.Format("2006-01-02") }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// span is "42 min", "1 h 5 min" or "40 s".
func span(seconds float64) string {
	s := int(math.Round(seconds))
	switch {
	case s < 90:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		return fmt.Sprintf("%d min", int(math.Round(float64(s)/60)))
	}
	h, m := s/3600, (s%3600+30)/60
	if m == 60 {
		h, m = h+1, 0
	}
	if m == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

func counted(ts []audio.Take) string {
	var secs float64
	for _, t := range ts {
		secs += t.Duration
	}
	return plural(len(ts), "take", "takes") + ", " + span(secs)
}

// days is the strip: the last n days, oldest first, a day with nothing in it
// there with zeros, so the strip's gaps are its quiet.
func days(ts []audio.Take, now time.Time, loc *time.Location, n int) []Day {
	byDay := map[string]*Day{}
	for _, t := range ts {
		k := dateKey(t.Created.In(loc))
		d := byDay[k]
		if d == nil {
			d = &Day{Date: k}
			byDay[k] = d
		}
		d.Count++
		d.Seconds += t.Duration
	}
	out := make([]Day, 0, n)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	for i := n - 1; i >= 0; i-- {
		k := dateKey(today.AddDate(0, 0, -i))
		if d := byDay[k]; d != nil {
			d.Seconds = math.Round(d.Seconds*10) / 10
			out = append(out, *d)
		} else {
			out = append(out, Day{Date: k})
		}
	}
	return out
}

// onThisDay: one card for each earlier year that holds takes from this date,
// the nearest year first. A leap day has its year only in leap years, as a
// birthday does; a 29 February take is not carried to the 28th.
func onThisDay(ts []audio.Take, now time.Time, loc *time.Location, opt Options) []Card {
	var out []Card
	for y := 1; y <= opt.OnThisDayYears; y++ {
		year := now.Year() - y
		var hit []audio.Take
		for _, t := range ts {
			c := t.Created.In(loc)
			if c.Year() == year && c.Month() == now.Month() && c.Day() == now.Day() {
				hit = append(hit, t)
			}
		}
		if len(hit) == 0 {
			continue
		}
		when := plural(y, "year", "years") + " ago"
		out = append(out, newCard("on-this-day:"+fmt.Sprint(year), OnThisDay, "On this day, "+when, counted(hit), hit, opt.PerCard))
	}
	return out
}

// sessions: takes made within SessionGap of the one before are one sitting.
// Sessions of the last SessionDays, newest first, named by the day and the part
// of it. A sitting of one take is still a sitting: it is what that day was.
func sessions(ts []audio.Take, now time.Time, loc *time.Location, opt Options) []Card {
	cutoff := now.AddDate(0, 0, -opt.SessionDays)
	var out []Card
	var cur []audio.Take
	flush := func() {
		if len(cur) == 0 {
			return
		}
		oldest := cur[len(cur)-1].Created.In(loc) // the sitting is named for where it began
		title := dayWord(oldest, now, loc) + " " + partOfDay(oldest)
		id := "session:" + oldest.Format("2006-01-02T15:04")
		out = append(out, newCard(id, Session, title, counted(cur), cur, opt.PerCard))
		cur = nil
	}
	for _, t := range ts { // newest first
		if t.Created.Before(cutoff) {
			break
		}
		if len(cur) > 0 && cur[len(cur)-1].Created.Sub(t.Created) > opt.SessionGap {
			flush()
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

func dayWord(t, now time.Time, loc *time.Location) string {
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	d := time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, loc)
	switch int(math.Round(today.Sub(d).Hours() / 24)) {
	case 0:
		return "Today"
	case 1:
		return "Yesterday"
	}
	if today.Sub(d) < 7*24*time.Hour {
		return t.Weekday().String()
	}
	return t.Format("Jan 2")
}

func partOfDay(t time.Time) string {
	switch h := t.Hour(); {
	case h < 5:
		return "late night"
	case h < 12:
		return "morning"
	case h < 17:
		return "afternoon"
	case h < 22:
		return "evening"
	}
	return "night"
}

// themes: takes of the last ThemeDays that share a tag, or a tempo (within
// 3 BPM, found by walking the sorted tempos), when there are ThemeMin of them.
// A take can be in a tag's theme and a tempo's: each is a way to find it.
func themes(ts []audio.Take, now time.Time, loc *time.Location, opt Options) []Card {
	cutoff := now.AddDate(0, 0, -opt.ThemeDays)
	var recent []audio.Take
	for _, t := range ts {
		if !t.Created.Before(cutoff) {
			recent = append(recent, t)
		}
	}
	var out []Card

	byTag := map[string][]audio.Take{}
	var tags []string
	for _, t := range recent {
		if t.Tag == "" {
			continue
		}
		if _, ok := byTag[t.Tag]; !ok {
			tags = append(tags, t.Tag)
		}
		byTag[t.Tag] = append(byTag[t.Tag], t)
	}
	for _, tag := range tags {
		if g := byTag[tag]; len(g) >= opt.ThemeMin {
			// The page knows the tag's name and colour; the card carries its id.
			c := newCard("tag:"+tag, Theme, "Takes with this tag", counted(g), g, opt.PerCard)
			c.Tag = tag
			out = append(out, c)
		}
	}

	var tempo []audio.Take
	for _, t := range recent {
		if t.BPM != nil && *t.BPM > 0 {
			tempo = append(tempo, t)
		}
	}
	sort.SliceStable(tempo, func(i, j int) bool { return *tempo[i].BPM < *tempo[j].BPM })
	for i := 0; i < len(tempo); {
		j := i + 1
		for j < len(tempo) && *tempo[j].BPM-*tempo[j-1].BPM <= 3 {
			j++
		}
		if g := tempo[i:j]; len(g) >= opt.ThemeMin {
			grp := append([]audio.Take(nil), g...)
			sort.SliceStable(grp, func(a, b int) bool { return grp[a].Created.After(grp[b].Created) })
			lo, hi := *g[0].BPM, *g[len(g)-1].BPM
			mid := int(math.Round((lo + hi) / 2))
			c := newCard(fmt.Sprintf("tempo:%d", mid), Theme, fmt.Sprintf("Around %d BPM", mid), counted(grp), grp, opt.PerCard)
			c.BPM = float64(mid)
			out = append(out, c)
		}
		i = j
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// suggestions are things worth doing, each to takes that show the need:
//   - label: a take of some length, past Settle, with no name of its own;
//   - star: a take marked twice or more (flags are where the player said
//     "this bit") and not starred, so it would be pushed down by newer ones;
//   - trim_silence: a take past LongTake that no selection narrows, which
//     likely has a stop in it (POST /api/take/trim-silence, where it exists).
//
// Cuts are left out of "label" (they are named by their span) and mixes are
// left out of all three: a bounce of a tape is that tape's.
func suggestions(ts []audio.Take, now time.Time, opt Options) []Card {
	var label, star, trim []audio.Take
	for _, t := range ts {
		if t.Origin == "tape" {
			continue
		}
		if t.Label == "" && t.Source == nil && t.Duration >= 10 && now.Sub(t.Created) >= opt.Settle {
			label = append(label, t)
		}
		if !t.Starred && len(t.Flags) >= 2 {
			star = append(star, t)
		}
		if t.Trim == nil && t.Duration >= opt.LongTake {
			trim = append(trim, t)
		}
	}
	var out []Card
	add := func(id, title, action string, g []audio.Take) {
		if len(g) == 0 {
			return
		}
		c := newCard(id, Suggestion, title, counted(g), g, opt.PerCard)
		c.Action = action
		out = append(out, c)
	}
	add("suggest:label", "Give these a name", "label", label)
	add("suggest:star", "You marked these more than once: star them?", "star", star)
	add("suggest:trim", "Long takes: cut out the quiet?", "trim_silence", trim)
	return out
}

// Title helper kept for tests and pages that want a plain-text line for a card.
func (c Card) String() string {
	return strings.TrimSpace(c.Title + ": " + c.Subtitle)
}
