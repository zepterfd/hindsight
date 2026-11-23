package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gabeduke/hindsight/internal/activity"
	"github.com/gabeduke/hindsight/internal/audio"
)

// GET /api/activity[?tz=America/New_York&days=60&per_card=6]
//
// The Listening Room's feed (internal/activity): sessions, themes, "on this
// day", a few suggested next steps and the strip of activity over time, made
// from the take list and nothing else. `tz` is the viewer's IANA zone (a take
// made at 11 pm is of that day for the person who made it); without it, or
// with one the Pi doesn't know, the Pi's own. It rides the list's ETag, so a
// page that polls it asks the Pi for nothing while no take has changed.
func (a *API) handleActivity(w http.ResponseWriter, r *http.Request) {
	takes, tag, err := a.takes.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	loc := time.Local
	if tz := q.Get("tz"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	var opt activity.Options
	if v, err := strconv.Atoi(q.Get("days")); err == nil && v > 0 && v <= 365 {
		opt.Days = v
	}
	if v, err := strconv.Atoi(q.Get("per_card")); err == nil && v > 0 && v <= 50 {
		opt.PerCard = v
	}

	// The feed changes with the clock as well as the takes (a take just kept
	// is "fresh" for an hour, a day turns), so the tag is the list's plus the
	// zone, the options and the day: an unchanged list on the same day is a 304.
	now := time.Now().In(loc)
	etag := `"` + tag + "-" + loc.String() + "-" + now.Format("20060102") + "-" +
		strconv.Itoa(opt.Days) + "-" + strconv.Itoa(opt.PerCard) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if takes == nil {
		takes = []audio.Take{}
	}
	writeJSON(w, http.StatusOK, activity.Build(takes, now, loc, opt))
}
