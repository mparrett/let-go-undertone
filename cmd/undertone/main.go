// undertone: a tiny generative-ambience engine. Architecturally a
// demo-sized homage to vsariola/sointu — no sointu code, just its shape: instruments are stacks of unit opcodes
// run by a tiny per-sample VM, a tracker-style sequencer drives them, and
// the whole track is derived deterministically from a seed.
//
// Register: roguelike ambience. Parameters derive from game state —
// mood (dungeon/overworld), level (depth darkens, elevation brightens),
// tension (tempo up, pulse in, harmony sours), and the seed, which also
// picks the lead instrument from a mood-weighted library. SetParameters
// applies a new map at the next pattern boundary, the same transactional
// semantics the letgo binding exposes as audio/set-parameters.
//
// This is the Go CLI front end; the engine lives in ../../engine and the
// let-go binding in ../../letgo.
//
//	go run ./cmd/undertone -play                          # live, endless, via oto
//	go run ./cmd/undertone -play -depth 5 -tension 0.8    # the abyss, something coming
//	go run ./cmd/undertone -play -mood overworld -depth 3 # bright mirror
//	go run ./cmd/undertone -wav out.wav -depth 4 -lead pluck  # deterministic render
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/ebitengine/oto/v3"

	"github.com/mparrett/let-go-undertone/engine"
)

func main() {
	play := flag.Bool("play", false, "play live (endless) via oto")
	wav := flag.String("wav", "", "render to WAV file")
	dur := flag.Float64("dur", 48, "WAV duration seconds")
	seed := flag.Uint64("seed", 1, "level seed")
	depth := flag.Int("depth", 0, "dungeon depth (negative = overworld elevation)")
	mood := flag.String("mood", "dungeon", "dungeon | overworld (elevation = depth)")
	lead := flag.String("lead", "", "lead instrument: bell|chime|clang|kalimba|reed|pluck (default: seed picks)")
	tension := flag.Float64("tension", 0, "0..1: tempo up, pulse in, harmony sours")
	vu := flag.String("vu", "bars", "meter style: bars | compact (one cell per band) | tall (two cells, 2x range)")
	flag.Parse()

	level := *depth
	if level < 0 { // signed-depth sugar: -3 means overworld elevation 3
		*mood, level = "overworld", -level
	}
	var p engine.Params
	switch *mood {
	case "overworld":
		p = engine.DeriveOverworld(*seed, level, *tension)
	default:
		p = engine.DeriveDungeon(*seed, level, *tension)
	}
	if *lead != "" {
		engine.ApplyLead(&p, *lead)
	}
	s := engine.NewSynth(p)
	desc := fmt.Sprintf("mood=%s seed=%d level=%d tension=%.2f lead=%s bpm=%.0f root=%d", *mood, *seed, level, *tension, p.LeadName, p.BPM, p.Root)
	switch {
	case *wav != "":
		if err := engine.WriteWAV(*wav, s, *dur); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("rendered %.0fs %s -> %s\n", *dur, desc, *wav)
	case *play:
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:   engine.SampleRate,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		<-ready
		pl := ctx.NewPlayer(io.Reader(s))
		pl.Play()
		// pl is unused from here, so without this the GC may collect the
		// handle mid-playback and oto's runtime.AddCleanup closes the player
		// — silence, no error. runMeterUI blocks for the process lifetime,
		// which is exactly how long the handle has to stay reachable.
		defer runtime.KeepAlive(pl)
		runMeterUI(s, desc, *vu)
	default:
		flag.Usage()
	}
}
