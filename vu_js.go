//go:build js

package main

import (
	"fmt"
	"syscall/js"
	"time"

	"undertone/engine"
)

// runMeterUI publishes band levels to window.__homageMeter ~30x/s for the
// host page to draw. Never returns. No terminal meters here: wasm_exec's
// stdout shim buffers until newline, so a \r-redraw line would never flush.
func runMeterUI(s *engine.Synth, desc, _ string) {
	fmt.Printf("playing %s\n", desc)
	go func() {
		for range time.Tick(33 * time.Millisecond) {
			v := s.Levels()
			js.Global().Set("__homageMeter", js.ValueOf([]any{v[0], v[1], v[2], v[3]}))
		}
	}()
	select {}
}
