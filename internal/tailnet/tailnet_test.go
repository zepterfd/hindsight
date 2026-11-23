package tailnet

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

// A made-up tailnet: no real name or address is in this repository.
const running = `{
  "Version": "1.80.0",
  "BackendState": "Running",
  "Self": {"DNSName": "hindsight-pi.tail0000.ts.net.", "Online": true, "TailscaleIPs": ["100.64.0.1"]},
  "Peer": {"a": {"Online": true}, "b": {"Online": false}, "c": {"Online": true}},
  "SomethingNew": {"x": 1}
}`

func TestParseRunning(t *testing.T) {
	s, err := Parse([]byte(running))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Installed || s.State != "Running" || s.DNSName != "hindsight-pi.tail0000.ts.net" || !s.Online || s.Peers != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestANodeThatIsNotRunningOffersNoName(t *testing.T) {
	s, _ := Parse([]byte(`{"BackendState":"NeedsLogin","Self":{"DNSName":"x.ts.net.","Online":true}}`))
	if s.DNSName != "" || s.Online || s.State != "NeedsLogin" {
		t.Fatalf("%+v", s)
	}
}

func TestParseRefusesNonJSON(t *testing.T) {
	if _, err := Parse([]byte("nope")); err == nil {
		t.Fatal("not json")
	}
}

func TestReadSeparatesNoProgramFromAProgramThatSaysNo(t *testing.T) {
	missing := func(context.Context, ...string) ([]byte, error) {
		return nil, &exec.Error{Name: "tailscale", Err: exec.ErrNotFound}
	}
	if s := Read(context.Background(), missing); s.Installed {
		t.Fatalf("%+v", s)
	}
	down := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	if s := Read(context.Background(), down); !s.Installed || s.State != "Stopped" || s.DNSName != "" {
		t.Fatalf("%+v", s)
	}
	junk := func(context.Context, ...string) ([]byte, error) { return []byte("<html>"), nil }
	if s := Read(context.Background(), junk); !s.Installed || s.State != "" {
		t.Fatalf("%+v", s)
	}
	var got []string
	ok := func(_ context.Context, args ...string) ([]byte, error) { got = args; return []byte(running), nil }
	if s := Read(context.Background(), ok); s.DNSName == "" || len(got) != 2 || got[0] != "status" || got[1] != "--json" {
		t.Fatalf("%+v %v", s, got)
	}
}
