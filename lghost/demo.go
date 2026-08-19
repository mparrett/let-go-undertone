package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/vm"
)

// runDemo walks demo.lg's scripted arc, one entry per turn.
//
// The tension accumulator lives HERE, in the host, not in lg — this function
// stands in for the browser audio module. lg supplies the pure step function
// (music/advance) and the host owns the one value that has to be remembered.
// That is the whole reason xsofy needs neither a world field nor a mutable
// global for tension: the memory belongs to the thing making the sound.
//
// Decay is applied once per parameter push, and pushes are turns, so the
// half-life is in game-clock units rather than seconds — idling does not
// relieve dread, taking turns does.
func runDemo(c *api.LetGo, m *music, dry bool, turnDur time.Duration) error {
	if _, err := c.Run("(require 'demo)"); err != nil {
		return err
	}
	n, err := evalInt(c, "(demo/turns)")
	if err != nil {
		return err
	}

	if !dry {
		m.start()
		if !m.started() {
			return fmt.Errorf("music-start did not open a player (see stderr)")
		}
		fmt.Println("playing the arc — parameter changes land at pattern boundaries (~8s),")
		fmt.Println("so the envelope is what makes each boundary sample representative.")
		fmt.Println()
	}

	tension := 0.0
	for i := 0; i < n; i++ {
		label, err := evalString(c, fmt.Sprintf("(demo/label %d)", i))
		if err != nil {
			return err
		}
		target, err := evalFloat(c, fmt.Sprintf("(music/tension-target (demo/world-at %d))", i))
		if err != nil {
			return err
		}
		// The envelope step is lg's, so the shape stays testable there.
		tension, err = evalFloat(c, fmt.Sprintf("(music/advance %v %v music/default-decay)", tension, target))
		if err != nil {
			return err
		}
		if _, err := c.Run(fmt.Sprintf(
			"(audio/set-parameters (music/params-for (demo/world-at %d) %v))", i, tension)); err != nil {
			return err
		}

		fmt.Printf("%2d  %-24s target %s  tension %s  bpm %3.0f  pulse %.2f\n",
			i+1, label, meter(target), meter(tension), m.params.BPM, m.params.PulseGain)

		if !dry {
			time.Sleep(turnDur)
		}
	}
	if !dry {
		m.stop()
	}
	return nil
}

// meter renders a 0..1 value as a short bar, so the attack/release asymmetry
// is visible in the trace and not just audible.
func meter(v float64) string {
	const w = 10
	full := int(math.Round(math.Min(math.Max(v, 0), 1) * w))
	return fmt.Sprintf("%s%s %.2f", strings.Repeat("#", full), strings.Repeat(".", w-full), v)
}

func evalFloat(c *api.LetGo, expr string) (float64, error) {
	v, err := c.Run(expr)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", expr, err)
	}
	switch n := v.(type) {
	case vm.Float:
		return float64(n), nil
	case vm.Int:
		return float64(n), nil
	}
	return 0, fmt.Errorf("%s: got %T, want a number", expr, v)
}

func evalInt(c *api.LetGo, expr string) (int, error) {
	f, err := evalFloat(c, expr)
	return int(f), err
}

func evalString(c *api.LetGo, expr string) (string, error) {
	v, err := c.Run(expr)
	if err != nil {
		return "", fmt.Errorf("%s: %w", expr, err)
	}
	s, ok := v.(vm.String)
	if !ok {
		return "", fmt.Errorf("%s: got %T, want a string", expr, v)
	}
	return string(s), nil
}
