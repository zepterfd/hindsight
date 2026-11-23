package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gabeduke/hindsight/internal/rig"
	"github.com/gabeduke/hindsight/internal/rig/catalog"
)

// The gear catalog (internal/rig/catalog):
//
//	GET  /api/catalog              what Hindsight knows of popular gear on a Pi
//	                               (the release's list, with CATALOG_URL's laid
//	                               over it), which have a profile to install and
//	                               which are installed
//	POST /api/catalog/install?id=  fetch that entry's profile, check its
//	                               SHA-256, and add it to the rig
//
// Nothing here is fetched until a person asks, and what is fetched is data:
// a catalog and a profile, neither run.

type catalogEntry struct {
	catalog.Entry
	Installed bool `json:"installed"`
	// Installable is true for an entry with a profile, when the Pi has a
	// CATALOG_URL it may be fetched from.
	Installable bool `json:"installable"`
}

type catalogResponse struct {
	Entries []catalogEntry `json:"entries"`
	// FetchError is why CATALOG_URL's list is missing, when it is: the
	// release's list is still here.
	FetchError string `json:"fetch_error,omitempty"`
	Source     string `json:"source"` // "release" or "release+remote"
}

func (a *API) catalogIndex(ctx context.Context) (catalog.Index, string, error) {
	seed, err := catalog.Seed()
	if err != nil {
		return catalog.Index{}, "", err
	}
	if a.cfg.CatalogURL == "" {
		return seed, "release", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	got, err := catalog.Client{}.Fetch(ctx, a.cfg.CatalogURL)
	if err != nil {
		return seed, "release", err
	}
	return catalog.Merge(seed, got), "release+remote", nil
}

func (a *API) handleCatalogGet(w http.ResponseWriter, r *http.Request) {
	ix, src, ferr := a.catalogIndex(r.Context())
	if len(ix.Entries) == 0 && ferr != nil {
		log.Printf("catalog: %v", ferr)
		writeErr(w, http.StatusInternalServerError, "could not read the catalog")
		return
	}
	have := catalog.Installed(a.cfg.ProfilesDir)
	resp := catalogResponse{Entries: []catalogEntry{}, Source: src}
	if ferr != nil {
		resp.FetchError = ferr.Error()
	}
	for _, e := range ix.Entries {
		resp.Entries = append(resp.Entries, catalogEntry{Entry: e, Installed: have[e.ID], Installable: e.Profile != nil})
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleCatalogInstall(w http.ResponseWriter, r *http.Request) {
	if a.cfg.CatalogURL == "" {
		writeErr(w, http.StatusNotImplemented, "no catalog is set up on this Pi (CATALOG_URL)")
		return
	}
	id := r.URL.Query().Get("id")
	ix, _, err := a.catalogIndex(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	var entry *catalog.Entry
	for i := range ix.Entries {
		if ix.Entries[i].ID == id {
			entry = &ix.Entries[i]
		}
	}
	if entry == nil {
		writeErr(w, http.StatusNotFound, "no such device in the catalog")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := catalog.Client{}.Install(ctx, *entry, a.cfg.ProfilesDir)
	switch {
	case errors.Is(err, catalog.ErrNoProfile):
		writeErr(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		log.Printf("catalog install %s: %v", id, err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, e := range rig.LoadDir(a.cfg.ProfilesDir) {
		log.Printf("[!] profiles: %v", e)
	}
	// The rig looked its interface's profile up once per device: look again.
	a.rig.mu.Lock()
	a.rig.profiles = nil
	a.rig.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"installed": true, "id": p.ID, "name": p.Name})
}
