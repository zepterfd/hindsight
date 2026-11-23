// Package tailnet says whether this Pi is on a Tailscale network, and by what
// name it can be reached there, by asking the tailscale CLI. It reads and
// reports; it never changes a Tailscale setting, and it never stores a
// tailnet's name or addresses (they are asked for each time, and shown, not
// kept in any file of the project).
//
// What it is for: a phone on the same tailnet reaches the Pi at a stable
// MagicDNS name from anywhere, so the app can offer *that* address (and a QR
// code for it) instead of an IP that changes, and the phone relay
// (docs/plan/phone-relay.md) knows where to send audio. See
// docs/plan/tailscale-routing.md.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// Status is what the app needs to know.
type Status struct {
	// Installed is false when there is no tailscale program.
	Installed bool `json:"installed"`
	// State is Tailscale's own word: "Running", "NeedsLogin", "Stopped"…
	State string `json:"state,omitempty"`
	// DNSName is this Pi's MagicDNS name, without the trailing dot: what a
	// phone on the tailnet types. Empty until it is on a tailnet with
	// MagicDNS.
	DNSName string `json:"dns_name,omitempty"`
	// Online is whether Tailscale says this node is connected now.
	Online bool `json:"online"`
	// Peers is how many other devices are on the tailnet and online.
	Peers int `json:"peers"`
}

// Runner runs `tailscale` with args and returns its stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// CLI runs the real tailscale program.
func CLI(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "tailscale", args...).Output()
}

// Read asks for the status, in two seconds at most: it is called from a page
// load, and a hung tailscaled must not hang the page.
func Read(ctx context.Context, run Runner) Status {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := run(ctx, "status", "--json")
	if err != nil {
		var nf *exec.Error
		if errors.As(err, &nf) {
			return Status{} // no tailscale program
		}
		// The program is here and said no (not running, not logged in, or
		// the daemon is down): installed, nothing more known.
		return Status{Installed: true, State: "Stopped"}
	}
	s, err := Parse(out)
	if err != nil {
		return Status{Installed: true}
	}
	return s
}

// Parse reads `tailscale status --json`. Only fields it needs are read, and an
// unknown shape is not an error: a newer Tailscale adds fields.
func Parse(b []byte) (Status, error) {
	var raw struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			DNSName string `json:"DNSName"`
			Online  bool   `json:"Online"`
		} `json:"Self"`
		Peer map[string]struct {
			Online bool `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Status{}, err
	}
	s := Status{Installed: true, State: raw.BackendState, Online: raw.Self.Online,
		DNSName: strings.TrimSuffix(raw.Self.DNSName, ".")}
	for _, p := range raw.Peer {
		if p.Online {
			s.Peers++
		}
	}
	// A node that is not Running has no name worth offering.
	if s.State != "Running" {
		s.DNSName = ""
		s.Online = false
	}
	return s, nil
}
