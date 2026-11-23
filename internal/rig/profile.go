// Package rig is the kit drawn as hardware
// (docs/superpowers/specs/2026-10-15-rig-design.md): devices as profiles,
// and cables that are settings. A profile says what a device's ports are
// and, for a USB interface, which capture channels are which tap and what
// each hears. The graph is built from the settings, the tape and what's
// plugged in each time it's asked for, and never stored: the picture can't
// disagree with what the Pi is doing.
package rig

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gabeduke/hindsight/internal/config"
)

// Port is a socket on a device.
type Port struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind is audio (a jack), usb (USB audio), midi, or sync (a pulse).
	Kind string `json:"kind"`
	// Dir is in, out or both.
	Dir string `json:"dir"`
	// In and Out are a USB port's channels.
	In    int    `json:"in,omitempty"`
	Out   int    `json:"out,omitempty"`
	About string `json:"about,omitempty"`
}

// Tap is a pair of an interface's capture channels: where it comes from on
// the device, and which of the tape's buses it hears.
type Tap struct {
	ID string `json:"id"`
	// USB is the pair, 1-indexed as the interface labels its channels.
	USB   [2]int   `json:"usb"`
	From  string   `json:"from,omitempty"` // a port id, or "mix"
	Hears []string `json:"hears,omitempty"`
	Pre   bool     `json:"pre,omitempty"` // before the strip's fader
	About string   `json:"about,omitempty"`
}

// Return is where a playback pair lands: the tape's bus played into it.
type Return struct {
	Bus   string `json:"bus"`
	USB   [2]int `json:"usb"`
	Into  string `json:"into"` // a port id
	About string `json:"about,omitempty"`
}

// Match is how a device is found: a USB audio card by its USB id, as ALSA's
// usbid file gives it; a MIDI port by a substring of its name, the rule
// MIDI_DEVICES uses.
type Match struct {
	USB  string `json:"usb,omitempty"`
	MIDI string `json:"midi,omitempty"`
}

// Profile is a device.
type Profile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Short  string `json:"short"`
	Maker  string `json:"maker,omitempty"`
	Match  Match  `json:"match"`
	Rate   int    `json:"rate,omitempty"`
	Format string `json:"format,omitempty"`
	// Source says where its numbers came from.
	Source string `json:"source,omitempty"`
	// Placeholder marks a profile waiting for its manual: it names only what
	// is known, and the rig guesses no port.
	Placeholder bool     `json:"placeholder,omitempty"`
	Note        string   `json:"note,omitempty"`
	Generic     bool     `json:"generic,omitempty"`
	Ports       []Port   `json:"ports"`
	Taps        []Tap    `json:"taps,omitempty"`
	Returns     []Return `json:"returns,omitempty"`
}

// Port finds a port by id.
func (p Profile) Port(id string) (Port, bool) {
	for _, x := range p.Ports {
		if x.ID == id {
			return x, true
		}
	}
	return Port{}, false
}

// TapAt finds the tap on a pair of channels (1-indexed).
func (p Profile) TapAt(usb [2]int) (Tap, bool) {
	for _, t := range p.Taps {
		if t.USB == usb {
			return t, true
		}
	}
	return Tap{}, false
}

//go:embed profiles/*.json
var profileFS embed.FS

var profiles = mustLoad()

func mustLoad() []Profile {
	ps, err := loadProfiles()
	if err != nil {
		panic(err)
	}
	return ps
}

// loadProfiles reads every embedded profile, refusing one that doesn't hang
// together: a tap or return naming a port it doesn't have, or a channel past
// its USB port's.
func loadProfiles() ([]Profile, error) {
	entries, err := profileFS.ReadDir("profiles")
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		b, err := profileFS.ReadFile(path.Join("profiles", e.Name()))
		if err != nil {
			return nil, err
		}
		var p Profile
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("profile %s: %w", e.Name(), err)
		}
		if err := p.check(); err != nil {
			return nil, fmt.Errorf("profile %s: %w", e.Name(), err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (p Profile) check() error {
	if p.ID == "" || p.Name == "" {
		return fmt.Errorf("needs an id and a name")
	}
	in, out := 0, 0
	if u, ok := p.Port("usb"); ok {
		in, out = u.In, u.Out
	}
	for _, t := range p.Taps {
		for _, c := range t.USB {
			if c < 1 || c > in {
				return fmt.Errorf("tap %s: channel %d isn't 1..%d", t.ID, c, in)
			}
		}
		if _, ok := p.Port(t.From); t.From != "mix" && !ok {
			return fmt.Errorf("tap %s: from %q isn't a port", t.ID, t.From)
		}
	}
	for _, r := range p.Returns {
		for _, c := range r.USB {
			if c < 1 || c > out {
				return fmt.Errorf("return %s: channel %d isn't 1..%d", r.Bus, c, out)
			}
		}
		if _, ok := p.Port(r.Into); !ok {
			return fmt.Errorf("return %s: into %q isn't a port", r.Bus, r.Into)
		}
	}
	return nil
}

// ParseProfile reads one profile file and refuses one that doesn't hang
// together, as the embedded ones are refused. It is the only way a profile
// from outside the release gets in, and it reads data: nothing in a profile
// is ever run.
func ParseProfile(b []byte) (Profile, error) {
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, fmt.Errorf("profile: %w", err)
	}
	if err := p.check(); err != nil {
		return Profile{}, fmt.Errorf("profile %s: %w", p.ID, err)
	}
	return p, nil
}

// The profiles an owner added (the catalog, internal/rig/catalog): data in a
// folder, read at start and again when one is installed. The release is the
// floor and an added profile only adds: one whose id a release profile has is
// ignored, so a bad file can't change how a device the release knows is drawn.
var (
	extraMu sync.RWMutex
	extra   []Profile
)

// LoadDir reads every *.json profile in dir into the added profiles, and
// returns what it could not read, one error per file, for the log. A missing
// folder is no profiles, and no error.
func LoadDir(dir string) []error {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		SetExtra(nil)
		return nil
	}
	if err != nil {
		return []error{err}
	}
	var ps []Profile
	var errs []error
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := ParseProfile(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		ps = append(ps, p)
	}
	SetExtra(ps)
	return errs
}

// SetExtra replaces the added profiles, dropping any a release profile owns
// the id of.
func SetExtra(ps []Profile) {
	own := map[string]bool{}
	for _, p := range profiles {
		own[p.ID] = true
	}
	var keep []Profile
	for _, p := range ps {
		if !own[p.ID] {
			keep = append(keep, p)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].ID < keep[j].ID })
	extraMu.Lock()
	extra = keep
	extraMu.Unlock()
}

// all is the release's profiles, then the added ones.
func all() []Profile {
	extraMu.RLock()
	defer extraMu.RUnlock()
	return append(append([]Profile(nil), profiles...), extra...)
}

// Profiles are the embedded profiles, then the ones added, by id.
func Profiles() []Profile { return all() }

// ForUSB finds the profile of an audio card by its USB id ("2367:9420").
func ForUSB(id string) (Profile, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return Profile{}, false
	}
	for _, p := range all() {
		if p.Match.USB != "" && strings.ToLower(p.Match.USB) == id {
			return p, true
		}
	}
	return Profile{}, false
}

// ForName finds a profile by how EXTRA_INPUTS may name its card: its USB id,
// or a name of it ("AMS-24", "AMS24", "Zoom AMS-24"), as config.SameCard
// matches them. A name of fewer than three letters names nothing ("24").
func ForName(name string) (Profile, bool) {
	if p, ok := ForUSB(name); ok {
		return p, true
	}
	if len(strings.TrimSpace(name)) < 3 {
		return Profile{}, false
	}
	for _, p := range all() {
		for _, x := range []string{p.Short, p.Match.MIDI} {
			if x != "" && config.SameCard(name, x) {
				return p, true
			}
		}
	}
	return Profile{}, false
}

// ForMIDI finds the profile of a MIDI port by its name: the profile's
// substring in the port's name, case-insensitively, as MIDI_DEVICES matches.
func ForMIDI(name string) (Profile, bool) {
	n := strings.ToLower(name)
	for _, p := range all() {
		if m := strings.ToLower(p.Match.MIDI); m != "" && strings.Contains(n, m) {
			return p, true
		}
	}
	return Profile{}, false
}

// GenericTapID names an unknown interface's tap on channels l and r.
func GenericTapID(l, r int) string {
	if l == r {
		return fmt.Sprintf("usb%d", l)
	}
	return fmt.Sprintf("usb%d-%d", l, r)
}

// Generic is an unknown interface's profile, from its channel counts: its
// taps named USB 1/2, 3/4… (an odd last channel on its own), and nothing
// assumed about what they hear or where anything returns.
func Generic(name string, in, out int) Profile {
	p := Profile{ID: "usb-audio", Name: name, Short: name, Generic: true,
		Note: "Not a device Hindsight has a profile for: its taps are its channels in pairs, and nothing is assumed about what they hear."}
	if p.Name == "" {
		p.Name, p.Short = "USB audio", "USB audio"
	}
	p.Ports = []Port{{ID: "usb", Label: "USB", Kind: "usb", Dir: "both", In: in, Out: out,
		About: fmt.Sprintf("USB audio: %d in, %d out.", in, out)}}
	for l := 1; l <= in; l += 2 {
		r := min(l+1, in)
		label := fmt.Sprintf("USB %d/%d", l, r)
		if l == r {
			label = fmt.Sprintf("USB %d", l)
		}
		p.Taps = append(p.Taps, Tap{ID: GenericTapID(l, r), USB: [2]int{l, r}, About: label + ", as the interface numbers its inputs."})
	}
	return p
}

// GenericMIDI is a MIDI device Hindsight has no profile for: MIDI in and
// out, by its name.
func GenericMIDI(name string) Profile {
	return Profile{ID: "midi", Name: name, Short: name, Generic: true, Match: Match{MIDI: name},
		Ports: []Port{
			{ID: "midi-in", Label: "MIDI IN", Kind: "midi", Dir: "in", About: "MIDI into " + name + "."},
			{ID: "midi-out", Label: "MIDI OUT", Kind: "midi", Dir: "out", About: "MIDI from " + name + "."},
		}}
}

// Sources is a profile's taps as TAPE_SOURCES spells them:
// name=L,R[:buses], space-separated.
func (p Profile) Sources() string {
	var parts []string
	for _, t := range p.Taps {
		s := fmt.Sprintf("%s=%d,%d", t.ID, t.USB[0], t.USB[1])
		if len(t.Hears) > 0 {
			s += ":" + strings.Join(t.Hears, "")
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}
