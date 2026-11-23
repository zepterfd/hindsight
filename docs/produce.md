# Producing a song

`hindsight-produce` turns stems (or several takes) into a finished file:
leveled against each other, mixed, brought to a loudness target with
headroom, faded, and written as WAV, FLAC or MP3. It needs `ffmpeg` on the
path and runs at `nice 10`, so run it when the rig is not recording.

```sh
go build -o hindsight-produce ./cmd/hindsight-produce
./hindsight-produce -o song.flac bass.wav drums.wav guitar.wav
./hindsight-produce -o song.mp3 -lufs -16 -fade-out 4 -trim 1=-2 take_*.wav
```

| Flag | Default | |
|---|---|---|
| `-o` | | `.wav` (24-bit), `.flac` or `.mp3` (256 kbps) |
| `-lufs` | `-14` | the mix's integrated loudness. Streaming services sit around -14 to -16 |
| `-tp` | `-1` | true-peak ceiling, dB |
| `-stem-lufs` | `-23` | the level each stem is brought to before the sum |
| `-highpass` | `30` | Hz; negative turns it off |
| `-fade-in`, `-fade-out` | `0` | seconds |
| `-trim INDEX=DB` | | a stem's own gain after leveling (repeatable): "the bass sits a little under" |
| `-json` | | print the measurements as JSON |

## What it does

1. **Levels each stem** to the same integrated loudness (EBU R128, ffmpeg's
   `loudnorm`), so a quiet bass and a hot guitar start even. A stem is moved
   by at most 18 dB, and a silent stem is left alone, so room tone is not
   boosted into a hiss.
2. **High-passes** each stem at 30 Hz: rumble costs headroom and says nothing.
3. **Sums** them with `amix` (normalisation off: the levels are set).
4. **Masters** with `loudnorm` in two-pass linear mode: the mix is measured,
   then moved to the target without the dynamics squash of its one-pass mode,
   with a true-peak ceiling and an `alimiter` as a safety net for the fades.
5. **Fades** and writes the file.

It is not a mixing engineer: no EQ, compression, panning or reverb. Those
are choices of taste, and the place for them is a `Plan` field and a filter
in `internal/produce`, where each is one line and a test; the `-trim` flag is
the one taste control for now.

## Layout

`internal/produce` is pure where it decides (`Plan.Mix`, `Plan.Master`,
`Plan.Args`, `ParseLoudness`) and shells out in one place (`FFmpeg`, a
`Runner` the tests replace). `produce_test.go` also runs the real ffmpeg on
two sines 40 dB apart and measures the result.

## Not done

- A button in the Listening Room: this is a command, not yet an API route or
  a sheet. A route (`POST /api/produce` with take names, answering a file
  through the renders queue in `internal/api`) is the next step.
- Stems from a take's channels: takes saved from a multi-input rig are one
  multichannel WAV; splitting by channel (`-map_channel`) belongs here too.
