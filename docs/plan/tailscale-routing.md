# Tailscale: a stable name for the Pi, reachable by the phone

**Status: proposed 2026-11-23, not agreed.** Gabe's note (11/18): *Tailscale
DNS service routing.* What this builds is the first step: ask Tailscale what
name this Pi has, offer it, and one script to put Hindsight on it. Routing
through a Tailscale **Service** (a name that outlives a replaced Pi) is the
step after, and is described, not built.

## Why

Today a phone reaches a Pi at an IP address on the room's WiFi, or through
the hub's HTTPS name (`docs/hub.md`), which resolves to a LAN address. Away
from the room (a phone on cellular, a second kit at a friend's), neither
works. Tailscale puts the phone and the Pi on one private network wherever
they are, and gives each a **MagicDNS name** that does not change when the IP
does. It also gives the Pi a real HTTPS certificate for that name, so the
phone's microphone, the installable app and the wake lock all work (they
need a secure origin), with nothing to host. The audio stays between your
own devices.

## What is built

- `internal/tailnet`: `tailscale status --json`, read; the node's MagicDNS
  name, whether it is online, how many peers. It changes no Tailscale
  setting and keeps nothing: a tailnet's name and addresses are asked for each
  time and shown, and appear in no file of this repository (tests use a made-up
  `tail0000.ts.net`).
- `GET /api/tailnet`: that status as JSON, for the app to offer the address
  and, later, a QR code for it, and for the phone relay to know where to send
  audio (`docs/plan/phone-relay.md`).
- `scripts/tailscale-serve.sh`: one command that makes tailscaled serve
  Hindsight at `https://<the Pi's name>/` on the tailnet, `--off` to stop,
  `--dry-run` to see the command. It uses `tailscale serve`, never `funnel`:
  **nothing is opened to the internet**, only devices on your tailnet can
  connect.

```sh
sudo tailscale up                       # once, to log the Pi in
scripts/tailscale-serve.sh              # https://<name> now reaches Hindsight
curl -s localhost:5000/api/tailnet      # {"installed":true,"state":"Running","dns_name":"…"}
```

In the admin console, MagicDNS and HTTPS certificates must be on for the
tailnet.

## What is not built, and the open decisions

1. **Services.** Tailscale's *Services* give a stable name (`svc:hindsight`)
   that routes to whichever device offers it, so a replaced or second Pi keeps
   the address the phones know. That is the "DNS service routing" of the
   note, and it is the better end state; it needs the tailnet's policy to
   define the service and tag the Pi, which is the owner's console, not
   something the app should do. The first step above works without it.
2. **How the phone learns the name.** A QR code on the rig page, or a line in
   Settings, from `/api/tailnet`. Not drawn.
3. **Security.** `serve` exposes Hindsight (which has no login of its own;
   the LAN is its trust boundary today) to every device on the tailnet. On a
   tailnet shared with people you do not trust, use Tailscale's access
   controls to limit who can reach the Pi, or do not serve it.
4. **WebSocket and large downloads** through `serve` are expected to work (it
   is a reverse proxy) but were not tried.

## Not verified

**No Tailscale was available in the session that wrote this.** The CLI
syntax in `tailscale-serve.sh` (`serve --bg --https=443 <target>`, `--https=443
off`) and the field names `tailnet` reads from `status --json`
(`BackendState`, `Self.DNSName`, `Self.Online`, `Peer`) are from the author's
knowledge of Tailscale, not run or checked against its docs here. The parser
is tested against a made-up document in that shape; the script is tested only
by `--dry-run`. Run it once on the Pi before relying on it.
