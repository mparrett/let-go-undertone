//go:build js

// grid is a step sequencer over the undertone engine: a 16-step pattern of
// lead, bass and pulse lanes, played by the engine's own tracker over its
// generated bed. The page draws the grid and owns its state; this program
// turns that state into engine calls and reports the playhead.
//
//	GOOS=js GOARCH=wasm go build -o examples/grid/main.wasm ./examples/grid
//	cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/grid/
//
// Serve the folder over HTTPS (or localhost): the browser output needs
// AudioWorklet, which Safari only offers in a secure context.
//
// JS surface, on window.undertoneGrid:
//
//	setGrid(json)    {lead:[16], bass:[16], pulse:[16], bed}: degrees 1..8, 0 = rest
//	setParams(json)  {bpm, mood: "overworld"|"dungeon", voice, seed}
//	seedGrid(seed)   a starting grid derived from the current params, as JSON
//	play() / stop()  call from the tap handler: iOS resumes audio only inside one
//	row()            the step being heard, or -1 while stopped
package main

import (
	"encoding/json"
	"io"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/mparrett/let-go-undertone/engine"
	"github.com/mparrett/let-go-undertone/webaudio"
)

// gate keeps the synth from advancing while stopped: the output may start
// on any tap (that is how iOS unlocks audio), and a grid should not begin
// playing until the play button says so.
type gate struct {
	src io.Reader
	on  atomic.Bool
}

func (g *gate) Read(p []byte) (int, error) {
	if !g.on.Load() {
		clear(p)
		return len(p), nil
	}
	return g.src.Read(p)
}

type gridJSON struct {
	Lead  []int  `json:"lead"`
	Bass  []int  `json:"bass"`
	Pulse []bool `json:"pulse"`
	Bed   bool   `json:"bed"`
}

type paramsJSON struct {
	BPM   float64 `json:"bpm"`
	Mood  string  `json:"mood"`
	Voice string  `json:"voice"`
	Seed  uint64  `json:"seed"`
}

func derive(c paramsJSON) engine.Params {
	var p engine.Params
	if c.Mood == "dungeon" {
		p = engine.DeriveDungeon(c.Seed, 2, 0)
	} else {
		p = engine.DeriveOverworld(c.Seed, 2, 0)
	}
	if c.Voice != "" {
		engine.ApplyLead(&p, c.Voice)
	}
	if c.BPM > 0 {
		p.BPM = min(max(c.BPM, 40), 200)
	}
	return p
}

func toJSON(g engine.Grid) string {
	out := gridJSON{Lead: g.Lead[:], Bass: g.Bass[:], Pulse: g.Pulse[:], Bed: g.Bed}
	b, _ := json.Marshal(out)
	return string(b)
}

func main() {
	cur := paramsJSON{BPM: 110, Mood: "overworld", Voice: "kalimba", Seed: 1}
	syn := engine.NewSynth(derive(cur))
	g0 := engine.SeedGrid(derive(cur))
	syn.SetGrid(&g0)
	src := &gate{src: syn}

	var player atomic.Pointer[webaudio.Player]
	go func() {
		// A shorter queue than webaudio's default: an edit is heard the next
		// time the playhead passes it, and every millisecond queued is a
		// millisecond the edit trails the playhead.
		p, err := webaudio.Open(src, webaudio.Options{SampleRate: engine.SampleRate, Queue: 200 * time.Millisecond})
		if err != nil {
			js.Global().Call("undertoneGridFailed", err.Error())
			return
		}
		player.Store(p)
		if !src.on.Load() {
			p.Pause()
		}
	}()

	api := map[string]any{
		"setGrid": js.FuncOf(func(_ js.Value, a []js.Value) any {
			var j gridJSON
			if json.Unmarshal([]byte(a[0].String()), &j) != nil {
				return nil
			}
			g := engine.Grid{Bed: j.Bed}
			copy(g.Lead[:], j.Lead)
			copy(g.Bass[:], j.Bass)
			copy(g.Pulse[:], j.Pulse)
			syn.SetGrid(&g)
			return nil
		}),
		"setParams": js.FuncOf(func(_ js.Value, a []js.Value) any {
			if json.Unmarshal([]byte(a[0].String()), &cur) == nil {
				syn.SetParametersNow(derive(cur))
			}
			return nil
		}),
		"seedGrid": js.FuncOf(func(_ js.Value, a []js.Value) any {
			p := derive(cur)
			p.Seed = uint64(a[0].Int())
			return toJSON(engine.SeedGrid(p))
		}),
		"play": js.FuncOf(func(js.Value, []js.Value) any {
			src.on.Store(true)
			if p := player.Load(); p != nil {
				p.Resume()
			}
			return nil
		}),
		"stop": js.FuncOf(func(js.Value, []js.Value) any {
			src.on.Store(false)
			if p := player.Load(); p != nil {
				p.Pause()
			}
			return nil
		}),
		"row": js.FuncOf(func(js.Value, []js.Value) any {
			p := player.Load()
			if p == nil || !src.on.Load() {
				return -1
			}
			// what is heard now: rendered minus what is still queued
			heard := syn.Frames() - int64(p.Queued().Seconds()*engine.SampleRate)
			return syn.RowAt(heard)
		}),
		"initial": toJSON(g0),
	}
	js.Global().Set("undertoneGrid", js.ValueOf(api))
	js.Global().Call("undertoneGridReady")
	select {}
}
