# Cue words

`web/static/lib/cue/`. A proof of concept: say **"Hindsight note"** (or
"Hindsight flag", "Hindsight song A") near a phone or tablet that has the
page open, and the Pi marks that moment in the feed, as the Note key does
(`POST /api/flag`). Hands stay on the instrument.

It is off until someone turns on **Cue words** in Settings, *Preview
features*. That is a per-device switch (`lib/cue/pref.js`, `lib/device-prefs.js`):
the microphone is this device's, and the other kits are untouched.

## Parts

| File | Does | Knows about |
|---|---|---|
| `parse.js` | transcript → cues. The wake word, then a verb. Pure | nothing |
| `listener.js` | wraps the browser's `SpeechRecognition`: counts a cue once per result, a 3 s cooldown, restarts when the recogniser stops on its own, gives up with `denied` when the microphone is refused | the recogniser (given in) |
| `mount.js` | a cue → `POST /api/flag` → a toast; mounted by `lib/nav.js` only when the switch is on | the Pi's API, toast |
| `pref.js` | the per-device switch | localStorage |

A new verb is one line in `parse.js`; a different recogniser (a Whisper
endpoint fed from `getUserMedia`, see `internal/transcribe`) is a different
`Recognition` handed to `createListener`.

## Cues

The wake word is required, so rehearsal chatter ("let's flag that bar")
marks nothing. It is matched loosely ("hind sight", "hindsite"), as a
recogniser writes a made-up word.

| Said | Does |
|---|---|
| hindsight flag / mark / bookmark / keep this | a note |
| hindsight note *words* | a note; *words* are heard as its label |
| hindsight song a | a note labelled "Song A" |

## Limits of the POC

- **Labels are heard but not kept.** The live flag (`POST /api/flag`) takes
  no label on the Pi, so the toast shows "Song A" and the note is plain. The
  step to keep it is a `label` on that endpoint and on the ring's flag
  (`internal/capture`), which is a change to the capture path and wants its
  own review.
- **The browser's recogniser is not private.** Chrome and Safari send the
  audio to their vendor's speech service, and the setting says so. It also
  needs the page open and the screen awake, and an https address (the hub,
  or localhost) for the microphone.
- **Latency.** Recognition answers a second or so after the words; the note
  lands at the moment the Pi receives it, not when it was said. A real
  version would stamp the request with the time the cue began and let the Pi
  subtract.
- **Not heard by the Pi.** The Pi's own audio is not listened to for cues:
  that would put speech recognition on the capture path's CPU.
- Not tried on a device: tested with a fake recogniser.
