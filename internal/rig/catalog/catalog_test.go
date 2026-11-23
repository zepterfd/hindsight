package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabeduke/hindsight/internal/rig"
)

const goodProfile = `{"id":"test-box","name":"Test Box","short":"Test","match":{"usb":"dead:beef"},
 "ports":[{"id":"usb","label":"USB","kind":"usb","dir":"both","in":2,"out":2}],
 "taps":[{"id":"t","usb":[1,2],"from":"mix"}]}`

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestTheSeedIsValidAndHonest(t *testing.T) {
	ix, err := Seed()
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) < 5 {
		t.Fatalf("seed has %d entries", len(ix.Entries))
	}
	for _, e := range ix.Entries {
		// A seed entry that claims to work must say who found out.
		if e.Linux == Works && e.Confidence == "" {
			t.Errorf("%s: works, by whose word?", e.ID)
		}
		if e.Profile != nil {
			t.Errorf("%s: the seed ships advice, not downloads", e.ID)
		}
	}
}

func TestParseRefusesBadCatalogs(t *testing.T) {
	entry := func(extra string) string {
		return `{"version":1,"entries":[{"id":"a","name":"A","maker":"M","kind":"interface","linux":"works","how":"h"` + extra + `}]}`
	}
	if _, err := Parse([]byte(entry(""))); err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"version":      `{"version":2,"entries":[]}`,
		"bad id":       strings.Replace(entry(""), `"id":"a"`, `"id":"../a"`, 1),
		"duplicate":    `{"version":1,"entries":[{"id":"a","name":"A","how":"h","linux":"works"},{"id":"a","name":"A","how":"h","linux":"works"}]}`,
		"status":       strings.Replace(entry(""), `"works"`, `"great"`, 1),
		"http page":    entry(`,"page":"http://x.example/"`),
		"no hash":      entry(`,"profile":{"url":"https://x.example/p.json","sha256":""}`),
		"non-hex hash": entry(`,"profile":{"url":"https://x.example/p.json","sha256":"` + strings.Repeat("z", 64) + `"}`),
		"http profile": entry(`,"profile":{"url":"http://x.example/p.json","sha256":"` + sum("x") + `"}`),
		"huge profile": entry(`,"profile":{"url":"https://x.example/p.json","sha256":"` + sum("x") + `","size":99999999}`),
		"not json":     `nope`,
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMergeLetsAFetchedEntryReplaceTheSeedsAndAddTheRest(t *testing.T) {
	a := Index{Version: 1, Entries: []Entry{{ID: "a", Name: "old"}, {ID: "b", Name: "b"}}}
	b := Index{Version: 1, Entries: []Entry{{ID: "a", Name: "new"}, {ID: "c", Name: "c"}}}
	m := Merge(a, b)
	if len(m.Entries) != 3 || m.Entries[0].Name != "new" || m.Entries[2].ID != "c" {
		t.Fatalf("%+v", m)
	}
}

func serve(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(b))
	}))
}

func entryFor(srv *httptest.Server, id, body string) Entry {
	return Entry{ID: id, Name: id, How: "h", Linux: Works,
		Profile: &ProfileRef{URL: srv.URL + "/p.json", SHA256: sum(body)}}
}

func TestInstallWritesAVerifiedProfileAndTheRigSeesIt(t *testing.T) {
	srv := serve(t, map[string]string{"/p.json": goodProfile})
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "profiles") // does not exist yet
	p, err := Client{}.Install(context.Background(), entryFor(srv, "test-box", goodProfile), dir)
	if err != nil || p.ID != "test-box" {
		t.Fatalf("%+v %v", p, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test-box.json"))
	if err != nil || string(b) != goodProfile {
		t.Fatalf("file: %q %v", b, err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("leftovers in %s: %v", dir, ents)
	}
	if !Installed(dir)["test-box"] {
		t.Fatal("Installed should list it")
	}

	t.Cleanup(func() { rig.SetExtra(nil) })
	if errs := rig.LoadDir(dir); len(errs) != 0 {
		t.Fatal(errs)
	}
	got, ok := rig.ForUSB("dead:beef")
	if !ok || got.ID != "test-box" {
		t.Fatal("the rig should find the installed profile by its USB id")
	}
}

func TestInstallRefusesAnythingThatIsNotTheFileNamed(t *testing.T) {
	srv := serve(t, map[string]string{"/p.json": goodProfile})
	defer srv.Close()
	dir := t.TempDir()
	c := Client{}

	e := entryFor(srv, "test-box", goodProfile)
	e.Profile.SHA256 = sum("something else")
	if _, err := c.Install(context.Background(), e, dir); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("hash mismatch: %v", err)
	}

	// A hash that matches a file that isn't a profile.
	srv2 := serve(t, map[string]string{"/p.json": `{"id":"x"}`})
	defer srv2.Close()
	if _, err := c.Install(context.Background(), entryFor(srv2, "x", `{"id":"x"}`), dir); err == nil {
		t.Fatal("a file with no name or ports is not a profile")
	}

	// An entry that names one device and delivers another's profile.
	if _, err := c.Install(context.Background(), entryFor(srv, "other-box", goodProfile), dir); err == nil || !strings.Contains(err.Error(), "says it is") {
		t.Fatalf("id mismatch: %v", err)
	}

	if _, err := c.Install(context.Background(), Entry{ID: "a"}, dir); err != ErrNoProfile {
		t.Fatalf("no profile: %v", err)
	}
	if _, err := c.Install(context.Background(), Entry{ID: "../etc", Profile: &ProfileRef{URL: srv.URL + "/p.json", SHA256: sum(goodProfile)}}, dir); err == nil {
		t.Fatal("a path as an id")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("nothing should be written on a refusal: %v", ents)
	}
}

func TestInstallLimitsTheDownloadAndHonoursStatus(t *testing.T) {
	big := strings.Repeat("x", MaxProfile+10)
	srv := serve(t, map[string]string{"/p.json": big})
	defer srv.Close()
	if _, err := (Client{}).Install(context.Background(), entryFor(srv, "big", big), t.TempDir()); err == nil || !strings.Contains(err.Error(), "over") {
		t.Fatalf("oversize: %v", err)
	}
	e := entryFor(srv, "gone", "x")
	e.Profile.URL = srv.URL + "/missing.json"
	if _, err := (Client{}).Install(context.Background(), e, t.TempDir()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestFetchReadsACatalogAndRefusesPlainHTTPElsewhere(t *testing.T) {
	doc := `{"version":1,"entries":[{"id":"a","name":"A","maker":"M","kind":"interface","linux":"works","how":"h"}]}`
	srv := serve(t, map[string]string{"/index.json": doc})
	defer srv.Close()
	ix, err := Client{}.Fetch(context.Background(), srv.URL+"/index.json")
	if err != nil || len(ix.Entries) != 1 {
		t.Fatalf("%+v %v", ix, err)
	}
	if _, err := (Client{}).Fetch(context.Background(), "http://example.com/index.json"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain http to another host: %v", err)
	}
}

func TestRigLoadDirKeepsTheReleaseAsTheFloor(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { rig.SetExtra(nil) })
	// A file that claims a release profile's id is ignored; a broken one is reported and skipped.
	shadow := strings.Replace(goodProfile, `"id":"test-box"`, `"id":"zoom-ams-24"`, 1)
	os.WriteFile(filepath.Join(dir, "zoom-ams-24.json"), []byte(shadow), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{`), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`x`), 0o644)
	errs := rig.LoadDir(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "broken.json") {
		t.Fatalf("errs: %v", errs)
	}
	if _, ok := rig.ForUSB("dead:beef"); ok {
		t.Fatal("a release profile's id must not be shadowed")
	}
	n := 0
	for _, p := range rig.Profiles() {
		if p.ID == "zoom-ams-24" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("zoom-ams-24 listed %d times", n)
	}
	if errs := rig.LoadDir(filepath.Join(dir, "nope")); errs != nil {
		t.Fatalf("a missing folder is no profiles, not an error: %v", errs)
	}
}
