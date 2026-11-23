// Command hindsight-tap streams raw audio from stdin to a Hindsight Pi as a
// recording, over the same resumable protocol the phone page uses
// (/api/phone). It is the software half of the inline tap
// (docs/plan/inline-tap.md): the box's job is to get audio to a program like
// this one; the Pi's job, as ever, is to keep it.
//
//	arecord -D hw:1 -f S16_LE -r 48000 -c 2 -t raw | hindsight-tap -url ws://hindsight.local:8080/api/phone
//	ffmpeg -i session.wav -f s16le -ac 2 -ar 48000 - | hindsight-tap -url … -rate 48000
//
// A WiFi blink loses nothing: it reconnects with the same recording id and
// resends from the chunk the Pi says it wants. End the input (^D, or the
// source exiting) and it stops the recording and prints the take's name.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/gabeduke/hindsight/internal/phoneclient"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("hindsight-tap", flag.ContinueOnError)
	url := fs.String("url", "", "the Pi's /api/phone address, ws://… or wss://…")
	rate := fs.Int("rate", 48000, "sample rate of the input")
	format := fs.String("format", "s16le", "input format: s16le, s32le or f32le")
	channels := fs.Int("channels", 2, "input channels: 1 or 2")
	id := fs.String("id", "", "recording id, to resume one (made if empty)")
	listen := fs.String("listen", "", "relay mode: instead of sending to the Pi, serve the audio live at ws://ADDR/stream for the phone page to relay (docs/plan/phone-relay.md); -url is not used")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" && *listen == "" {
		fmt.Fprintln(os.Stderr, "hindsight-tap: -url (send to the Pi) or -listen (serve to a phone) is required")
		return 2
	}
	chunks, readErr, err := phoneclient.PCM{Format: *format, Channels: *channels, Rate: *rate}.Chunks(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *listen != "" {
		fmt.Fprintf(os.Stderr, "serving live audio at ws://%s/stream\n", *listen)
		l := &phoneclient.Live{Rate: *rate}
		if err := l.Serve(ctx, *listen, chunks); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	s := &phoneclient.Sender{URL: *url, Rate: *rate, ID: *id,
		OnState: func(st string) { fmt.Fprintln(os.Stderr, st) }}
	fmt.Fprintf(os.Stderr, "recording id %s\n", orNew(s))
	res, err := s.Run(ctx, chunks)
	select {
	case e := <-readErr:
		fmt.Fprintln(os.Stderr, "input:", e)
	default:
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if res.Name == "" {
		fmt.Println("nothing was recorded")
		return 0
	}
	fmt.Printf("%s (%.0f s%s)\n", res.Name, res.Seconds, map[bool]string{true: ", partial"}[res.Partial])
	return 0
}

// orNew gives the sender an id now, so it can be printed (and used to
// resume) before the first byte is sent.
func orNew(s *phoneclient.Sender) string {
	if s.ID == "" {
		s.ID = phoneclient.NewID()
	}
	return s.ID
}
