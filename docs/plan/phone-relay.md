# The phone as the relay

**Status: proposed 2026-11-23, not agreed.** Gabe's note (11/18): *phone needs
to be the relay via Tailscale.* A POC of the relay itself is built; the page
that hosts it is not.

## The problem

The inline tap (`docs/plan/inline-tap.md`) is a small box with WiFi. The
Pi may be somewhere the box cannot reach: on another network, or only on a
tailnet (`docs/plan/tailscale-routing.md`), which a box with no screen and no
way to log in cannot join. **A phone can be on both.** It is next to the
box, and it reaches the Pi wherever the Pi is.

```
 tap ── ws://tap.local:9000/stream ──► phone page ── wss://<pi>/api/phone ──► Pi
        (the room's WiFi)              (Relay → Uploader)   (the tailnet, or the hub)
```

## What is built

- `web/static/lib/phone/relay.js`: `Relay` connects to the tap's stream,
  waits for its `hello` (the sample rate), cuts the audio into the
  tenth-of-a-second chunks the Pi wants, and hands them to the **same
  `Uploader` the phone recorder uses**. So the Pi's end is unchanged:
  numbering, acks, resend after a dropout, the ten-minute grace, the partial
  take. The relay owns only the first hop.
- A tap that drops is reconnected to, and **the recording stays open**: a WiFi
  blink at the box is a gap in a take, not the end of one. A tap that returns
  at a different sample rate is refused (the Pi's recording has one rate).
  `stop()` flushes what is left and resolves with the saved take.
- `hindsight-tap -listen :9000`: the tap's side of that stream, `hello` then
  binary stereo float32, live, one client at a time (a new one replaces the
  old). It keeps nothing: audio with no phone connected is dropped.

Tested with fakes at both ends (`relay.test.js`, `live_test.go`) and **end to
end by hand**: `ffmpeg … | hindsight-tap -listen` → `Relay` + `Uploader` in
node → a running demo Pi, which saved the take.

## What is not built, and what will bite

1. **The page.** Nothing opens a `Relay` yet: it needs a screen with the tap's
   address, a start/stop, a meter and the saved take's name, in the rooms'
   words. It is the next PR, and wants a decision on where it lives (the
   recorder's page, or the Rig page next to the tap's own card).
2. **Mixed content.** A page served over **https** (the hub, or Tailscale
   `serve`) may not open a plain `ws://` connection to a tap on the LAN: the
   browser blocks it. Options: serve the relay page over **http** from the
   Pi's LAN address while in the room (the Pi's `ws://` phone address then
   works too, but the page has no secure origin and no microphone, which a
   relay does not need); or give the tap a `wss://` certificate (a hub-issued
   name, or a local CA), which a box with no screen has to be provisioned
   with. For a phone on cellular reaching a Pi over the tailnet, with a tap
   on the room's WiFi, **the phone is not on the tap's network**, and this
   path does not apply: the tap needs to be near the phone.
3. **A phone's tab in a pocket.** Browsers throttle or suspend a background
   tab; the recorder uses a wake lock, and the relay would too. A screen-off
   relay on iOS is not something a web page can promise. If it matters, the
   relay is a small native app, or the tap talks to the Pi directly and the
   phone is only the hotspot.
4. **The tap's `-listen` accepts any origin** and accepts no input. It sends
   sound to anyone on the network who connects, so it is a LAN-only mode.
