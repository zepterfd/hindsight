package activity

import (
	"fmt"
	"testing"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

var zone = time.FixedZone("EST", -5*3600)

// now is Monday 2026-11-23, 10:00 in the zone.
var now = time.Date(2026, 11, 23, 10, 0, 0, 0, zone)

func at(days int, hour, min int) time.Time {
	d := now.AddDate(0, 0, -days)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, zone)
}

func take(name string, created time.Time, secs float64) audio.Take {
	return audio.Take{Name: name, Created: created, Duration: secs, Label: name}
}

func cardsOf(f Feed, k Kind) []Card {
	var out []Card
	for _, c := range f.Cards {
		if c.Kind == k {
			out = append(out, c)
		}
	}
	return out
}

func TestSessionsSplitOnAQuietGapAndAreNamedForWhereTheyBegan(t *testing.T) {
	ts := []audio.Take{
		take("a", at(1, 20, 0), 60), take("b", at(1, 20, 40), 120), take("c", at(1, 21, 30), 30), // yesterday evening
		take("d", at(0, 8, 30), 90), // this morning
		take("e", at(3, 14, 0), 45), // Friday afternoon
	}
	f := Build(ts, now, zone, Options{})
	got := cardsOf(f, Session)
	if len(got) != 3 {
		t.Fatalf("sessions = %+v", got)
	}
	if got[0].Title != "Today morning" || got[0].Count != 1 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Title != "Yesterday evening" || got[1].Count != 3 || got[1].Subtitle != "3 takes, 4 min" {
		t.Errorf("second = %+v", got[1])
	}
	if got[1].Takes[0] != "c" || got[1].Takes[2] != "a" {
		t.Errorf("takes should be newest first: %v", got[1].Takes)
	}
	if got[2].Title != "Friday afternoon" {
		t.Errorf("third = %+v", got[2])
	}
	if !got[0].At.After(got[1].At) {
		t.Error("cards are newest first")
	}
}

func TestASessionPastMidnightIsNamedForTheDayItBegan(t *testing.T) {
	ts := []audio.Take{take("a", at(1, 23, 30), 60), take("b", at(0, 0, 40), 60)}
	got := cardsOf(Build(ts, now, zone, Options{}), Session)
	if len(got) != 1 || got[0].Title != "Yesterday night" {
		t.Fatalf("got %+v", got)
	}
}

func TestSessionsOlderThanTheWindowAreLeftOut(t *testing.T) {
	ts := []audio.Take{take("old", at(20, 12, 0), 60), take("new", at(1, 12, 0), 60)}
	got := cardsOf(Build(ts, now, zone, Options{}), Session)
	if len(got) != 1 || got[0].Takes[0] != "new" {
		t.Fatalf("got %+v", got)
	}
}

func TestOnThisDayFindsTheSameDateInEarlierYears(t *testing.T) {
	ts := []audio.Take{
		take("y1", time.Date(2025, 11, 23, 19, 0, 0, 0, zone), 60),
		take("y1b", time.Date(2025, 11, 23, 20, 0, 0, 0, zone), 60),
		take("y3", time.Date(2023, 11, 23, 9, 0, 0, 0, zone), 30),
		take("wrong-day", time.Date(2025, 11, 22, 19, 0, 0, 0, zone), 60),
		take("today", at(0, 8, 0), 60),
	}
	got := cardsOf(Build(ts, now, zone, Options{}), OnThisDay)
	if len(got) != 2 {
		t.Fatalf("cards = %+v", got)
	}
	if got[0].Title != "On this day, 1 year ago" || got[0].Count != 2 || got[0].ID != "on-this-day:2025" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Title != "On this day, 3 years ago" || got[1].Takes[0] != "y3" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestADayIsTheDayInTheCallersZone(t *testing.T) {
	// 02:00 UTC on the 24th is 21:00 on the 23rd in the zone.
	late := time.Date(2025, 11, 24, 2, 0, 0, 0, time.UTC)
	got := cardsOf(Build([]audio.Take{take("late", late, 60)}, now, zone, Options{}), OnThisDay)
	if len(got) != 1 {
		t.Fatalf("a take made at 9 pm on the 23rd is of the 23rd: %+v", got)
	}
	if utc := cardsOf(Build([]audio.Take{take("late", late, 60)}, now.UTC(), time.UTC, Options{}), OnThisDay); len(utc) != 0 {
		t.Errorf("in UTC it is the 24th: %+v", utc)
	}
}

func TestThemesGatherATagAndATempoOnlyAtThreeOrMore(t *testing.T) {
	bpm := func(v float64) *float64 { return &v }
	ts := []audio.Take{
		{Name: "t1", Created: at(1, 10, 0), Duration: 60, Label: "x", Tag: "g1", BPM: bpm(96)},
		{Name: "t2", Created: at(2, 10, 0), Duration: 60, Label: "x", Tag: "g1", BPM: bpm(97.5)},
		{Name: "t3", Created: at(5, 10, 0), Duration: 60, Label: "x", Tag: "g1", BPM: bpm(95)},
		{Name: "t4", Created: at(6, 10, 0), Duration: 60, Label: "x", Tag: "g2", BPM: bpm(140)},
		{Name: "t5", Created: at(7, 10, 0), Duration: 60, Label: "x", Tag: "g2"},
		{Name: "far", Created: at(100, 10, 0), Duration: 60, Label: "x", Tag: "g1", BPM: bpm(96)},
	}
	got := cardsOf(Build(ts, now, zone, Options{}), Theme)
	if len(got) != 2 {
		t.Fatalf("themes = %+v", got)
	}
	var tag, tempo *Card
	for i := range got {
		if got[i].Tag != "" {
			tag = &got[i]
		} else {
			tempo = &got[i]
		}
	}
	if tag == nil || tag.Tag != "g1" || tag.Count != 3 {
		t.Errorf("tag theme = %+v (g2 has two, and a take older than 60 days does not count)", tag)
	}
	if tempo == nil || tempo.BPM != 96 || tempo.Count != 3 || tempo.Title != "Around 96 BPM" {
		t.Errorf("tempo theme = %+v", tempo)
	}
}

func TestTemposMoreThanThreeApartAreNotOneTheme(t *testing.T) {
	bpm := func(v float64) *float64 { return &v }
	ts := []audio.Take{
		{Name: "a", Created: at(1, 10, 0), Duration: 60, Label: "x", BPM: bpm(90)},
		{Name: "b", Created: at(2, 10, 0), Duration: 60, Label: "x", BPM: bpm(95)},
		{Name: "c", Created: at(3, 10, 0), Duration: 60, Label: "x", BPM: bpm(100)},
	}
	if got := cardsOf(Build(ts, now, zone, Options{}), Theme); len(got) != 0 {
		t.Errorf("90, 95 and 100 are not one tempo: %+v", got)
	}
}

func TestSuggestionsNeedAShownNeed(t *testing.T) {
	ts := []audio.Take{
		{Name: "unnamed", Created: at(1, 10, 0), Duration: 40},
		{Name: "fresh", Created: now.Add(-10 * time.Minute), Duration: 40}, // still being looked at
		{Name: "named", Created: at(1, 11, 0), Duration: 40, Label: "riff"},
		{Name: "cut", Created: at(1, 12, 0), Duration: 40, Source: &audio.CutSource{Name: "unnamed"}}, // named by its span
		{Name: "tiny", Created: at(1, 13, 0), Duration: 3},                                            // not worth a name
		{Name: "marked", Created: at(1, 14, 0), Duration: 40, Label: "m", Flags: []audio.Flag{{Frame: 1}, {Frame: 2}}},
		{Name: "markedStar", Created: at(1, 15, 0), Duration: 40, Label: "m", Starred: true, Flags: []audio.Flag{{Frame: 1}, {Frame: 2}}},
		{Name: "long", Created: at(1, 16, 0), Duration: 600, Label: "l"},
		{Name: "longTrimmed", Created: at(1, 17, 0), Duration: 600, Label: "l", Trim: &audio.Trim{StartFrame: 1, EndFrame: 2}},
		{Name: "bounce", Created: at(1, 18, 0), Duration: 600, Origin: "tape"},
	}
	byAction := map[string][]string{}
	for _, c := range cardsOf(Build(ts, now, zone, Options{}), Suggestion) {
		byAction[c.Action] = c.Takes
	}
	if got := fmt.Sprint(byAction["label"]); got != "[unnamed]" {
		t.Errorf("label = %v", got)
	}
	if got := fmt.Sprint(byAction["star"]); got != "[marked]" {
		t.Errorf("star = %v", got)
	}
	if got := fmt.Sprint(byAction["trim_silence"]); got != "[long]" {
		t.Errorf("trim_silence = %v (a trimmed take and a bounce are left out)", got)
	}
}

func TestTheStripCoversEveryDayOldestFirstWithGapsAsZeros(t *testing.T) {
	ts := []audio.Take{take("a", at(0, 8, 0), 60), take("b", at(0, 9, 0), 30), take("c", at(2, 9, 0), 10)}
	f := Build(ts, now, zone, Options{Days: 5})
	if len(f.Days) != 5 {
		t.Fatalf("days = %+v", f.Days)
	}
	want := []Day{{"2026-11-19", 0, 0}, {"2026-11-20", 0, 0}, {"2026-11-21", 1, 10}, {"2026-11-22", 0, 0}, {"2026-11-23", 2, 90}}
	for i := range want {
		if f.Days[i] != want[i] {
			t.Errorf("day %d = %+v, want %+v", i, f.Days[i], want[i])
		}
	}
}

func TestATakeFromTheFutureIsIgnoredAndAnEmptyLibraryIsAnEmptyFeed(t *testing.T) {
	f := Build([]audio.Take{take("future", now.Add(48*time.Hour), 60)}, now, zone, Options{})
	if len(f.Cards) != 0 || len(f.Days) != 60 {
		t.Errorf("feed = %d cards, %d days", len(f.Cards), len(f.Days))
	}
	f = Build(nil, now, nil, Options{})
	if f.Cards == nil || len(f.Cards) != 0 || f.Zone != "UTC" {
		t.Errorf("empty = %+v", f)
	}
}

func TestSpanWords(t *testing.T) {
	for in, want := range map[float64]string{40: "40 s", 89: "89 s", 90: "2 min", 2520: "42 min", 3600: "1 h", 3900: "1 h 5 min", 7199: "2 h"} {
		if got := span(in); got != want {
			t.Errorf("span(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPerCardCapsTheTakesButNotTheCount(t *testing.T) {
	var ts []audio.Take
	for i := 0; i < 10; i++ {
		ts = append(ts, take(fmt.Sprintf("t%d", i), at(1, 10, i), 10))
	}
	got := cardsOf(Build(ts, now, zone, Options{PerCard: 4}), Session)
	if len(got) != 1 || got[0].Count != 10 || len(got[0].Takes) != 4 {
		t.Fatalf("got %+v", got)
	}
}
