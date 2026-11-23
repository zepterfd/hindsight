package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gabeduke/hindsight/internal/config"
	"github.com/gabeduke/hindsight/internal/tailnet"
	"github.com/gorilla/mux"
)

func TestTailnetReportsTheNameAPhoneCanUse(t *testing.T) {
	a := New(&config.Config{OutputDir: t.TempDir()}, nil, nil, nil, nil)
	t.Cleanup(a.WaitBackground)
	a.tailscale = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"BackendState":"Running","Self":{"DNSName":"pi.tail0000.ts.net.","Online":true},"Peer":{}}`), nil
	}
	r := mux.NewRouter()
	a.SetupRoutes(r)
	w := do(t, r, http.MethodGet, "/api/tailnet")
	var s tailnet.Status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil || s.DNSName != "pi.tail0000.ts.net" || !s.Online {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
