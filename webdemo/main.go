//go:build js

// webdemo is the browser front end of the lghost demo: the identical host
// surface (undertone/host), the identical lg lane — loaded from embedded
// sources via load-string, since GOOS=js has no filesystem — and the same
// scripted arc, with the trace going to the page instead of stdout.
//
// This is also the first browser proof of the ONE-SHOT lane (the engine-only
// web/ page predates sfx): music parameters and sfx triggers both crossing
// the lg->Go boundary inside wasm, out through oto's AudioWorklet backend.
//
//	GOOS=js GOARCH=wasm go build -o webdemo/main.wasm ./webdemo
//	python3 serve.py 8901 webdemo
package main

import (
	"fmt"
	"syscall/js"
	"time"

	"github.com/nooga/let-go/pkg/api"

	"undertone/engine"
	"undertone/host"
)

var doc = js.Global().Get("document")

func setText(id, s string) { doc.Call("getElementById", id).Set("textContent", s) }

func appendLine(s string) {
	pre := doc.Call("getElementById", "trace")
	pre.Set("textContent", pre.Get("textContent").String()+s+"\n")
	pre.Set("scrollTop", pre.Get("scrollHeight"))
}

func main() {
	m := host.NewMusic(engine.DeriveDungeon(1, 0, 0))
	host.Install(m)

	c, err := api.NewLetGo("undertone.web")
	if err != nil {
		setText("status", "let-go failed to boot: "+err.Error())
		return
	}
	if err := host.LoadEmbedded(c); err != nil {
		setText("status", "lg load failed: "+err.Error())
		return
	}

	// Open the audio device NOW, at load, and let Start block in the
	// background: oto's ready channel only closes on a touchend/keyup/mouseup
	// that arrives AFTER the AudioContext exists. Creating the context inside
	// the play handler is the trap — the tap's own touchend has already fired
	// by then, so ready never closes and Start hangs forever. Opened at load,
	// the play tap itself is the unlocking gesture.
	go func() {
		m.Start()
		fmt.Println("webdemo: audio device open:", m.Started())
	}()
	setText("status", "ready — tap to play the arc")

	running := false
	btn := doc.Call("getElementById", "play")
	btn.Set("disabled", false)
	btn.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
		if running {
			return nil
		}
		running = true
		// Everything after the gesture happens on a goroutine: blocking a JS
		// callback deadlocks the wasm scheduler, and oto's context must be
		// created close to the gesture for the autoplay policy.
		go func() {
			defer func() { running = false }()
			runArc(c, m)
		}()
		return nil
	}))

	select {} // keep the wasm module alive for the event handlers
}

func runArc(c *api.LetGo, m *host.Music) {
	setText("status", "unlocking audio…")
	// The tap that invoked us is also the gesture oto is waiting on; its
	// resume round-trip is a microtask away, so this settles almost at once.
	for i := 0; !m.Started(); i++ {
		if i == 40 { // ~4s: something else is wrong (no output device?)
			setText("status", "audio never unlocked — tap once more, or see console")
		}
		time.Sleep(100 * time.Millisecond)
	}
	setText("status", "playing — parameter changes land at pattern boundaries (~8s)")
	setText("trace", "")
	n, err := host.DemoTurns(c)
	if err != nil {
		setText("status", err.Error())
		return
	}
	tension := 0.0
	for i := 0; i < n; i++ {
		var line string
		tension, line, err = host.DemoStep(c, m, i, tension)
		if err != nil {
			setText("status", fmt.Sprintf("turn %d: %v", i+1, err))
			return
		}
		appendLine(line)
		time.Sleep(1800 * time.Millisecond)
	}
	// Deliberately no Stop: closing the player would strand a replay tap
	// (the device only unlocks on a gesture oto sees AFTER creation). The
	// last scene keeps humming, which is also the nicer ending.
	setText("status", "done — the deep keeps playing; tap to run the arc again")
}
