//go:build !js

package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// runMeterUI redraws a single carriage-return line of band meters ~30x/s.
// Never returns; ctrl-c kills the process.
func runMeterUI(s *Synth, desc string) {
	fmt.Printf("playing %s — ctrl-c to stop\n", desc)
	const width = 12
	names := [4]string{"sub", "low", "mid", "hi"}
	for range time.Tick(33 * time.Millisecond) {
		var b strings.Builder
		b.WriteString("\r ")
		for i, v := range s.Levels() {
			// perceptual-ish curve so quiet ambience still moves the bar
			n := int(math.Pow(v, 0.4)*width + 0.5)
			if n > width {
				n = width
			}
			fmt.Fprintf(&b, "%-3s %s%s  ", names[i],
				strings.Repeat("█", n), strings.Repeat("·", width-n))
		}
		fmt.Print(b.String())
	}
}
