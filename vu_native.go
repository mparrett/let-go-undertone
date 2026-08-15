//go:build !js

package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"
)

const vuWidth = 12

// empty-track glyph; try "🮐" (legacy-computing checkerboard) if your
// terminal font carries it — Fairfax HD does, most terminal fonts don't
const vuEmpty = "·"

// eighth-width partial blocks for the bar tip, smooth sub-cell motion
var vuTips = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

func vuColor(cell int) string {
	switch {
	case cell < 8:
		return "\x1b[32m" // green
	case cell < 10:
		return "\x1b[33m" // yellow
	default:
		return "\x1b[31m" // red
	}
}

// bar renders one band: colored zone fill with a fractional tip, dim empty
// track, and a peak-hold tick that decays slower than the bar.
func bar(b *strings.Builder, v, peak float64, color bool) {
	f := math.Min(math.Pow(v, 0.4)*vuWidth, vuWidth)
	full := int(f)
	tip := int((f - float64(full)) * 8)
	peakCell := int(math.Min(math.Pow(peak, 0.4)*vuWidth, vuWidth-1))
	for c := 0; c < vuWidth; c++ {
		if color {
			b.WriteString(vuColor(c))
		}
		switch {
		case c < full:
			b.WriteString("█")
		case c == full && tip > 0:
			b.WriteString(vuTips[tip])
		case c == peakCell && peakCell > full:
			b.WriteString("╹")
		default:
			if color {
				b.WriteString("\x1b[2m")
			}
			b.WriteString(vuEmpty)
		}
		if color {
			b.WriteString("\x1b[0m")
		}
	}
}

func isTTY() bool {
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// runMeterUI redraws a single carriage-return line of band meters ~30x/s.
// Never returns; ctrl-c restores the cursor and exits.
func runMeterUI(s *Synth, desc string) {
	fmt.Printf("playing %s — ctrl-c to stop\n", desc)
	tty := isTTY()
	if tty {
		fmt.Print("\x1b[?25l")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		go func() {
			<-sig
			fmt.Print("\x1b[?25h\n")
			os.Exit(0)
		}()
	}
	names := [4]string{"sub", "low", "mid", "hi"}
	var peaks [4]float64
	for range time.Tick(33 * time.Millisecond) {
		var b strings.Builder
		b.WriteString("\r ")
		for i, v := range s.Levels() {
			peaks[i] = math.Max(v, peaks[i]*0.97)
			if tty {
				b.WriteString("\x1b[2m")
			}
			fmt.Fprintf(&b, "%-3s ", names[i])
			if tty {
				b.WriteString("\x1b[0m")
			}
			bar(&b, v, peaks[i], tty)
			b.WriteString("  ")
		}
		fmt.Print(b.String())
	}
}
