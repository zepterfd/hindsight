# The inline tap

**Status: proposed 2026-11-23, not agreed.** Gabe's note (11/18): *an inline
"man in the middle" hardware device that connects to WiFi and transmits only
sound, to record, e.g. for a whole session.* This is the design, and a
working half: `cmd/hindsight-tap` and `internal/phoneclient` send audio to a
Pi today. The box itself is not built.

## The idea

A small box that sits **in the signal path between an instrument and
whatever it already plugs into** (an amp, a mixer channel, the Sidekick),
listens, and sends the sound to the Pi over WiFi. It carries no controls and
no screen; the Pi is where recording is managed. It exists so the
instrument that is *not* plugged into the Pi's interface (a guitar going
straight to an amp, a keyboard into a PA) still ends up on tape.

## The rule that decides the design: the player's signal never depends on the box

An inline device that can break a gig is worse than none. So:

- **The through path is analog and passive-first.** In → out is a wire (a
  TS jack pair, or XLR pass-through) with the tap taken off it through a
  high-impedance buffer. Dead battery, crashed firmware, no WiFi: the
  instrument still plays. No relay that has to *close* for sound to flow.
- **The tap never loads the signal.** An op-amp buffer (input impedance
  ≥1 MΩ for an instrument; a bridging input for a line) ahead of the ADC.
- **It transmits only sound.** No inbound control, no remote access: the box
  opens one outbound WebSocket to the Pi and answers nothing else. Nothing on
  it is a server.

## What is inside (a sketch to cost, not a bill of materials)

| Block | Option | Why |
|---|---|---|
| Brain + WiFi | Raspberry Pi Zero 2 W, or an ESP32-S3 | The Zero runs `hindsight-tap` as it is; the ESP32 is cheaper and lower-power but needs the protocol ported (it is a WebSocket, JSON, and numbered float32 chunks) |
| ADC | A stereo I²S codec (24-bit) with line/instrument gain | Any 24-bit I²S ADC the Pi's kernel has a driver for; keep it off the brain's supply noise |
| Buffer | JFET or low-noise op-amp, with a clip LED | The thing that keeps a guitar's tone; the one analog part worth prototyping first |
| Power | USB-C, with a small battery for a stage | A 10,000 mAh pack runs a Zero 2 W for most of a day |
| Storage | None | Audio is not kept on the box: it is the Pi's. See *If the WiFi drops*, below |

## How the audio gets to the Pi

The Pi already has the receiving end: `GET /api/phone`, a WebSocket that a
phone records into, in numbered stereo float32 chunks that the Pi acks and
that can be resent after a dropout. The tap is "a phone with no screen".
`internal/phoneclient` is that protocol's Go end:

```sh
arecord -D hw:1 -f S16_LE -r 48000 -c 2 -t raw \
  | hindsight-tap -url ws://hindsight.local:8080/api/phone
```

A recording id names the session, not the connection: **a WiFi blink loses
nothing**. The box reconnects with the same id, the Pi says which chunk it
wants next, and the box resends from there. The Pi saves a take when the
sender stops, or, for a sender that never comes back, as a partial take after
ten minutes.

### If the WiFi drops for longer than the box can hold

The sender keeps up to 10 minutes of audio unacked in RAM (`MaxQueueChunks`)
and then drops the *oldest*; the Pi starts the take from what is held and
says so (`partial`). A longer outage wants storage on the box (an SD-card
spool the sender drains) and is the first thing to add once the box exists.

## "A whole session"

The phone protocol has a length limit (`audio.PhoneMaxSeconds`). A session
longer than that is several takes in a row: the sender stops and restarts
with a new id when the Pi ends a take with reason `limit`. **That rollover is
not built**; `Run` returns at the first limit.

## Where it joins the network

1. **The jam space's WiFi** (the Pi and the box on one router). Simplest.
2. **The Pi's own hotspot**, for a space with no router.
3. **Through a phone**, when the box cannot be on the tailnet but a phone can:
   `docs/plan/phone-relay.md`.

Plain `ws://` on a LAN is fine for sound; across the internet it is `wss://`
only, through the hub or Tailscale (`docs/plan/tailscale-routing.md`).

## What to build, in order

1. **Prototype the buffer + ADC on a bench** with a Pi Zero 2 W and any I²S
   codec board; record a guitar; compare the tap's recording to the amp's
   sound for tone and noise floor. This is the part that can fail.
2. A **session rollover** in `phoneclient` (above).
3. A **spool** for long outages.
4. A case, a jack pair, a clip LED, and a pairing story (how the box learns
   the Pi's address: a config file on the SD card is enough).

## Not done / not verified

- No hardware was built or tested. The sender is verified against the Pi's
  real handler in a test (a link cut mid-recording resumes; every chunk lands
  once, in order) and by hand against a running demo Pi.
- The ESP32 option needs the protocol ported; nothing is written for it.
