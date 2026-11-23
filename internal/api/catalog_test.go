package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/config"
	"github.com/gabeduke/hindsight/internal/rig"
	"github.com/gorilla/mux"
)

const catProfile = `{"id":"cat-box","name":"Cat Box","short":"Cat","match":{"usb":"cafe:f00d"},"ports":[{"id":"usb","label":"USB","kind":"usb","dir":"both","in":2,"out":2}]}`

func catalogAPI(t *testing.T, url string) (*mux.Router, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "profiles")
	a := New(&config.Config{OutputDir: t.TempDir(), ProfilesDir: dir, CatalogURL: url}, nil, nil, nil, nil)
	t.Cleanup(a.WaitBackground)
	t.Cleanup(func() { rig.SetExtra(nil) })
	r := mux.NewRouter()
	a.SetupRoutes(r)
	return r, dir
}

func TestCatalogListsTheReleaseOffline(t *testing.T) {
	r, _ := catalogAPI(t, "")
	w := do(t, r, http.MethodGet, "/api/catalog")
	var resp catalogResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &resp) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if resp.Source != "release" || len(resp.Entries) < 5 || resp.FetchError != "" {
		t.Fatalf("%+v", resp)
	}
	if w := do(t, r, http.MethodPost, "/api/catalog/install?id=zoom-r20"); w.Code != http.StatusNotImplemented {
		t.Fatalf("install with no CATALOG_URL: %d", w.Code)
	}
}

func TestCatalogInstallsFromARemoteListAndTheRigSeesIt(t *testing.T) {
	sum := sha256.Sum256([]byte(catProfile))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			fmt.Fprintf(w, `{"version":1,"entries":[{"id":"cat-box","name":"Cat Box","maker":"M","kind":"interface","linux":"works","how":"plug it in","profile":{"url":"%s/p.json","sha256":"%s"}}]}`, srv.URL, hex.EncodeToString(sum[:]))
		case "/p.json":
			w.Write([]byte(catProfile))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r, dir := catalogAPI(t, srv.URL+"/index.json")

	var resp catalogResponse
	w := do(t, r, http.MethodGet, "/api/catalog")
	json.Unmarshal(w.Body.Bytes(), &resp)
	var found *catalogEntry
	for i := range resp.Entries {
		if resp.Entries[i].ID == "cat-box" {
			found = &resp.Entries[i]
		}
	}
	if resp.Source != "release+remote" || found == nil || !found.Installable || found.Installed {
		t.Fatalf("%+v", resp)
	}

	if w := do(t, r, http.MethodPost, "/api/catalog/install?id=nope"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", w.Code)
	}
	if w := do(t, r, http.MethodPost, "/api/catalog/install?id=zoom-r20"); w.Code != http.StatusConflict {
		t.Fatalf("an entry with no profile: %d", w.Code)
	}
	w = do(t, r, http.MethodPost, "/api/catalog/install?id=cat-box")
	if w.Code != 200 {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "cat-box.json")); err != nil {
		t.Fatal(err)
	}
	if p, ok := rig.ForUSB("cafe:f00d"); !ok || p.ID != "cat-box" {
		t.Fatal("the rig should know the installed profile at once")
	}
	w = do(t, r, http.MethodGet, "/api/catalog")
	resp = catalogResponse{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	for _, e := range resp.Entries {
		if e.ID == "cat-box" && !e.Installed {
			t.Fatal("should now read as installed")
		}
	}
}

func TestCatalogKeepsTheReleaseListWhenTheRemoteIsDown(t *testing.T) {
	r, _ := catalogAPI(t, "http://127.0.0.1:1/index.json")
	w := do(t, r, http.MethodGet, "/api/catalog")
	var resp catalogResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != 200 || resp.FetchError == "" || len(resp.Entries) < 5 {
		t.Fatalf("%d %+v", w.Code, resp)
	}
}
