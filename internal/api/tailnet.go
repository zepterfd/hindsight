package api

import (
	"net/http"

	"github.com/gabeduke/hindsight/internal/tailnet"
)

// GET /api/tailnet says whether this Pi is on a Tailscale network and by what
// name a phone on it can reach it (internal/tailnet): the app offers that
// address, not an IP that changes. Read-only; asked of the tailscale program
// each time and never stored.
func (a *API) handleTailnet(w http.ResponseWriter, r *http.Request) {
	run := a.tailscale
	if run == nil {
		run = tailnet.CLI
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, tailnet.Read(r.Context(), run))
}
