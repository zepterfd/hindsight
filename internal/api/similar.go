package api

import (
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gabeduke/hindsight/internal/similar"
)

// Takes that play alike (internal/similar):
//
//	GET /api/take/similar?file=[&min=0.8][&limit=10]   takes like this one, best first
//	GET /api/takes/groups[?min=0.85]                   every group of takes that play alike
//
// Both read the fingerprints the background analysis has already stored
// (the take's "harmony" section); neither reads audio. `pending` counts the
// takes with no fingerprint yet, so a page can say the answer is still being
// worked out.

// library is every take that has a fingerprint, in list order, and how many
// have none yet.
func (a *API) library() (entries []similar.Entry, pending int, err error) {
	takes, _, err := a.takes.List()
	if err != nil {
		return nil, 0, err
	}
	for _, t := range takes {
		if t.Duration <= 0 {
			continue
		}
		if p, ok := similar.Load(filepath.Join(a.cfg.OutputDir, t.Name)); ok {
			entries = append(entries, similar.Entry{Name: t.Name, Print: p})
		} else {
			pending++
		}
	}
	return entries, pending, nil
}

func queryFloat(r *http.Request, key string, def, lo, hi float64) float64 {
	v, err := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	if err != nil || v < lo || v > hi {
		return def
	}
	return v
}

func (a *API) handleSimilar(w http.ResponseWriter, r *http.Request) {
	name, _, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	entries, pending, err := a.library()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list takes")
		return
	}
	var target *similar.Entry
	for i := range entries {
		if entries[i].Name == name {
			target = &entries[i]
		}
	}
	resp := map[string]any{"file": name, "pending": pending, "matches": []similar.Match{}}
	if target != nil {
		resp["key"] = target.Print.Key
		resp["progression"] = target.Print.Changes >= similar.MinChanges
		min := queryFloat(r, "min", 0.8, 0, 1)
		limit := int(queryFloat(r, "limit", 10, 1, 100))
		resp["matches"] = similar.Rank(*target, entries, min, limit)
	} else {
		resp["analysed"] = false
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleGroups(w http.ResponseWriter, r *http.Request) {
	entries, pending, err := a.library()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list takes")
		return
	}
	groups := similar.Groups(entries, queryFloat(r, "min", 0.85, 0, 1))
	if groups == nil {
		groups = []similar.Group{}
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "pending": pending})
}
