//go:build js

// tuner is a standalone bench for tuning xsofy's sfx by ear: fire any sound on
// demand, sweep the parameters that shape it, and hear it against a real music
// bed — without hunting for a monster in a real run.
//
// The point of it is fidelity. It does NOT reimplement the vocabulary: it
// fetches xsofy's own music.lg and sfx.lg over HTTP and loads them into an
// embedded let-go, so every sound is produced by the exact code the game
// ships. Change a gain in xsofy/sfx.lg, reload the page, hear the change. A
// bench with its own copy of the curves would drift from the game and quietly
// tune the wrong thing.
//
// JS surface (all installed on window):
//
//	XsofyTunerLoad(name, src)  load one lg namespace, in dependency order
//	XsofyTunerFire(descriptor) run the real vocabulary on one event descriptor
//	                           and play the resulting triggers; returns the
//	                           trigger maps as JSON so the page can show them
//	XsofyTunerMusic(depth, tension, on)  set the bed the sfx are judged against
//
//	GOOS=js GOARCH=wasm go build -o tuner/main.wasm ./tuner
package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/vm"

	"undertone/engine"
	"undertone/host"
)

var (
	lg    *api.LetGo
	music *host.Music
	fired int // salts each fire, so repeats vary the way consecutive turns do
)

// lgLiteral renders a JS descriptor object as an lg map literal. Only the
// keys the vocabulary reads are passed through, and each is written with an
// explicit type, so a JS number never arrives as something lg will not accept.
func lgLiteral(d js.Value) string {
	var b strings.Builder
	b.WriteString("{:kind :")
	b.WriteString(d.Get("kind").String())
	for _, k := range []string{"severity", "dist", "dir"} {
		if v := d.Get(k); v.Type() == js.TypeNumber {
			fmt.Fprintf(&b, " :%s %s", k, strconv.FormatFloat(v.Float(), 'f', 4, 64))
		}
	}
	if v := d.Get("killed"); v.Type() == js.TypeBoolean && v.Bool() {
		b.WriteString(" :killed true")
	}
	// :broke is meaningful in both states (a web you did not tear is quieter,
	// not silent), so unlike :killed it is written even when false.
	if v := d.Get("broke"); v.Type() == js.TypeBoolean {
		fmt.Fprintf(&b, " :broke %t", v.Bool())
	}
	if v := d.Get("dirOfTravel"); v.Type() == js.TypeString {
		fmt.Fprintf(&b, " :dir-of-travel :%s", v.String())
	}
	b.WriteString("}")
	return b.String()
}

// fire runs the real xsofy.sfx vocabulary on one descriptor and plays what it
// returns. The returned string is the trigger maps as the vocabulary produced
// them — the page shows it, so the numbers being tuned are visible and not
// only audible.
func fire(d js.Value) string {
	fired++
	expr := fmt.Sprintf(
		"(let [shots (xsofy.sfx/for-event %s (xsofy.det/combine %d [:tuner]))]"+
			"  (audio/trigger shots) (pr-str shots))",
		lgLiteral(d), fired)
	v, err := lg.Run(expr)
	if err != nil {
		return "error: " + err.Error()
	}
	if s, ok := v.(vm.String); ok {
		return string(s)
	}
	return fmt.Sprint(v)
}

// setMusic puts a real bed under the sfx. Judging a one-shot in silence is
// the classic way to pick a gain that turns out wrong in the game, where it
// competes with a drone; `on false` mutes the bed for the dry comparison.
func setMusic(depth int, tension float64, on bool) {
	p := engine.DeriveDungeon(1, depth, tension)
	if !on {
		p.DroneGain, p.FifthGain, p.PadDensity = 0, 0, 0
		p.BellDensity, p.AirGain, p.PulseGain = 0, 0, 0
	}
	music.SetParams(p)
}

func main() {
	music = host.NewMusic(engine.DeriveDungeon(1, 1, 0))
	host.Install(music)

	c, err := api.NewLetGo("xsofy.tuner")
	if err != nil {
		js.Global().Call("XsofyTunerFailed", err.Error())
		return
	}
	lg = c

	js.Global().Set("XsofyTunerLoad", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) < 2 {
			return "missing args"
		}
		if err := host.LoadSource(c, a[0].String(), a[1].String()); err != nil {
			return err.Error()
		}
		return ""
	}))
	js.Global().Set("XsofyTunerFire", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) < 1 {
			return ""
		}
		return fire(a[0])
	}))
	js.Global().Set("XsofyTunerMusic", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) < 3 {
			return nil
		}
		setMusic(a[0].Int(), a[1].Float(), a[2].Bool())
		return nil
	}))

	// Open the device now and let it block in the background: oto's ready
	// channel only closes on a gesture that arrives AFTER the AudioContext
	// exists, so creating it inside the first tap handler waits forever for a
	// gesture that has already happened.
	go music.Start()

	// Supervisor loop, and NOT a `select {}`. With every goroutine parked —
	// main in the select, Start on oto's ready channel — the wasm runtime
	// declares "all goroutines are asleep - deadlock" and exits the program,
	// after which every call from the page hits a dead module. A pending
	// timer is what keeps the runtime from concluding that, so the thing that
	// keeps this alive may as well do the job of reporting readiness.
	reported := false
	for {
		time.Sleep(200 * time.Millisecond)
		if !reported && music.Started() {
			reported = true
			js.Global().Call("XsofyTunerReady", true)
		}
	}
}
