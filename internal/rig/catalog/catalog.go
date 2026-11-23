// Package catalog is the list of gear Hindsight knows how to live with, and
// the way to add a device's profile to a Pi without a release.
//
// "Driver download" is the wrong frame for a Raspberry Pi: almost all the
// gear a band owns is USB class-compliant (it works with the kernel's own
// audio driver, no download), a mixer's remote-control apps (Mixing Station,
// the maker's own) run on a phone or a laptop and talk to the mixer over the
// network, and a vendor driver for a Mac or Windows does nothing on the Pi. So
// an Entry says, for each device, which of those it is, in words, with a link
// to the maker. The one thing that *can* be installed is a rig profile
// (internal/rig): data that tells Hindsight what a device's channels are.
// An entry that has one carries its address and its SHA-256; Install fetches it,
// checks the hash, and refuses anything that doesn't parse as a profile.
//
// Nothing here runs a downloaded file. A catalog entry is data about a
// device; an installed profile is data read by the rig; no step executes
// either. (docs/hardware/README.md, *Later: sharing profiles*, is this.)
package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gabeduke/hindsight/internal/rig"
)

// Linux says how a device fares on the Pi.
type Linux string

const (
	Works       Linux = "works"       // the kernel's own driver: plug it in
	Partial     Linux = "partial"     // audio works; something else (its mixer, its settings) needs more
	Network     Linux = "network"     // not a Pi device at all: it is controlled over the network
	Unsupported Linux = "unsupported" // known not to work
	Unknown     Linux = "unknown"     // nobody has tried it, or said so
)

// Entry is one device.
type Entry struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Maker string `json:"maker"`
	// Kind: interface, mixer, recorder, controller or app.
	Kind  string `json:"kind"`
	Linux Linux  `json:"linux"`
	// How is the answer in words: what plugs in where, what to download and
	// where it runs. Written as advice, not as a promise.
	How string `json:"how"`
	// Page is the maker's (or app's) own address, to read; never fetched.
	Page string `json:"page,omitempty"`
	// Confidence is where How came from: "sourced" (the maker's page or a
	// manual), "owner" (someone ran it on a Pi) or "unconfirmed" (reported).
	Confidence string `json:"confidence"`
	// Profile, if the entry has one to install.
	Profile *ProfileRef `json:"profile,omitempty"`
}

// ProfileRef is a downloadable profile.
type ProfileRef struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// Index is a catalog file.
type Index struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

//go:embed index.json
var seed []byte

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// MaxProfile is the most a profile download may be: a profile is a screen of
// JSON, so a megabyte is a mistake.
const MaxProfile = 256 << 10

// Seed is the catalog that ships in the release.
func Seed() (Index, error) { return Parse(seed) }

// Parse reads a catalog and refuses a bad one whole.
func Parse(b []byte) (Index, error) {
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return Index{}, fmt.Errorf("catalog: %w", err)
	}
	if ix.Version != 1 {
		return Index{}, fmt.Errorf("catalog: version %d is not one this Hindsight reads", ix.Version)
	}
	seen := map[string]bool{}
	for _, e := range ix.Entries {
		if !idRE.MatchString(e.ID) {
			return Index{}, fmt.Errorf("catalog: %q is not an id (lower-case letters, digits and hyphens)", e.ID)
		}
		if seen[e.ID] {
			return Index{}, fmt.Errorf("catalog: %q twice", e.ID)
		}
		seen[e.ID] = true
		if e.Name == "" || e.How == "" {
			return Index{}, fmt.Errorf("catalog: %s needs a name and a how", e.ID)
		}
		switch e.Linux {
		case Works, Partial, Network, Unsupported, Unknown:
		default:
			return Index{}, fmt.Errorf("catalog: %s: %q is not a Linux status", e.ID, e.Linux)
		}
		if e.Page != "" {
			if u, err := url.Parse(e.Page); err != nil || u.Scheme != "https" || u.Host == "" {
				return Index{}, fmt.Errorf("catalog: %s: page must be an https address", e.ID)
			}
		}
		if p := e.Profile; p != nil {
			if len(p.SHA256) != 64 {
				return Index{}, fmt.Errorf("catalog: %s: a profile needs its SHA-256", e.ID)
			}
			if _, err := hex.DecodeString(p.SHA256); err != nil {
				return Index{}, fmt.Errorf("catalog: %s: the SHA-256 is not hex", e.ID)
			}
			if p.Size > MaxProfile {
				return Index{}, fmt.Errorf("catalog: %s: the profile is %d bytes, over the %d limit", e.ID, p.Size, MaxProfile)
			}
			if err := checkURL(p.URL); err != nil {
				return Index{}, fmt.Errorf("catalog: %s: %w", e.ID, err)
			}
		}
	}
	sort.Slice(ix.Entries, func(i, j int) bool { return ix.Entries[i].ID < ix.Entries[j].ID })
	return ix, nil
}

// Merge is the seed with a fetched catalog laid over it: a fetched entry
// replaces the seed's of the same id, and adds the rest.
func Merge(seed, fetched Index) Index {
	by := map[string]Entry{}
	for _, e := range seed.Entries {
		by[e.ID] = e
	}
	for _, e := range fetched.Entries {
		by[e.ID] = e
	}
	out := Index{Version: 1}
	for _, e := range by {
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].ID < out.Entries[j].ID })
	return out
}

// checkURL allows https, and http only to this machine (for tests and for a
// catalog served from the Pi's own folder): a profile is not worth trusting
// over a wire anyone can edit, even with a hash, because the index that
// carries the hash came over the same wire.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an address", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if ip := net.ParseIP(u.Hostname()); (ip != nil && ip.IsLoopback()) || u.Hostname() == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("%q must be https", raw)
}

// Client fetches and installs.
type Client struct {
	HTTP *http.Client // a 30-second client if nil
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Client) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := checkURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: %s: %s", raw, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("catalog: %s is over %d bytes", raw, limit)
	}
	return b, nil
}

// Fetch reads a catalog from its address.
func (c Client) Fetch(ctx context.Context, indexURL string) (Index, error) {
	b, err := c.get(ctx, indexURL, 1<<20)
	if err != nil {
		return Index{}, err
	}
	return Parse(b)
}

// ErrNoProfile is Install of an entry that has none to install.
var ErrNoProfile = errors.New("catalog: that entry has no profile to install")

// Install fetches the entry's profile, checks it is the file the catalog
// named (SHA-256) and that it is a profile the rig accepts, and writes it to
// dir as <id>.json, atomically. It returns the profile. Nothing is written
// unless every check passes. The profile's id must be the entry's, so an
// entry can't install over another device's profile.
func (c Client) Install(ctx context.Context, e Entry, dir string) (rig.Profile, error) {
	if e.Profile == nil {
		return rig.Profile{}, ErrNoProfile
	}
	if !idRE.MatchString(e.ID) {
		return rig.Profile{}, fmt.Errorf("catalog: %q is not an id", e.ID)
	}
	b, err := c.get(ctx, e.Profile.URL, MaxProfile)
	if err != nil {
		return rig.Profile{}, err
	}
	sum := sha256.Sum256(b)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), e.Profile.SHA256) {
		return rig.Profile{}, fmt.Errorf("catalog: %s: the download is not the file the catalog named (SHA-256 differs); nothing installed", e.ID)
	}
	p, err := rig.ParseProfile(b)
	if err != nil {
		return rig.Profile{}, err
	}
	if p.ID != e.ID {
		return rig.Profile{}, fmt.Errorf("catalog: %s: the profile says it is %q", e.ID, p.ID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rig.Profile{}, err
	}
	tmp, err := os.CreateTemp(dir, ".profile-*")
	if err != nil {
		return rig.Profile{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, bytes.NewReader(b)); err != nil {
		tmp.Close()
		return rig.Profile{}, err
	}
	if err := tmp.Close(); err != nil {
		return rig.Profile{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return rig.Profile{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, e.ID+".json")); err != nil {
		return rig.Profile{}, err
	}
	return p, nil
}

// Installed lists the ids that have a profile file in dir.
func Installed(dir string) map[string]bool {
	out := map[string]bool{}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && idRE.MatchString(id) {
			out[id] = true
		}
	}
	return out
}
