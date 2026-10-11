package engine

import "math"

// line is one parameter's response to level: base + step*level, clamped.
// The clamps are ±Inf where a mood leaves a parameter unbounded, and
// math.Max and math.Min pass a value through those exactly.
type line struct{ base, step, lo, hi float64 }

func lin(base, step float64) line { return line{base, step, math.Inf(-1), math.Inf(1)} }

func (l line) atLeast(lo float64) line { l.lo = lo; return l }
func (l line) atMost(hi float64) line  { l.hi = hi; return l }

// at evaluates the line at a level. The float64 conversion stops the compiler
// fusing the multiply-add, which Go may do on arm64; unfused, the result is
// the same on every architecture and in the lg port.
func (l line) at(x float64) float64 {
	return math.Min(l.hi, math.Max(l.lo, l.base+float64(l.step*x)))
}

// mood is a patch bank plus a point in parameter space, and how that point
// moves as the level rises. Tension works the same in every mood, so it is
// not in the table: tempo up, pulse in, the second drone sliding to a tritone
// and a minor ninth, the calm layers thinning.
type mood struct {
	name     string
	scales   [][]int // indexed by level/2; the last one holds for higher levels
	bpm      line
	rootBase int
	rootStep int
	drone    line
	fifth    line
	pad      line
	bell     line
	air      line
	calm     int // the second drone's interval below the chord at low tension
	bellLift int
	palette  Palette
	leads    []string // the seed picks one; repeats are the weights
}

var moods = []mood{
	// Depth darkens: dorian -> aeolian -> phrygian, slower, lower, thicker.
	{
		name:     "dungeon",
		scales:   scalesByDarkness,
		bpm:      lin(66, -3).atLeast(46),
		rootBase: 57, rootStep: -1,
		drone:   lin(0.55, 0.06).atMost(0.9),
		fifth:   lin(0, 0.2).atMost(0.7),
		pad:     lin(0.6, -0.05),
		bell:    lin(0.16, -0.02).atLeast(0.04),
		air:     lin(0.25, 0.05).atMost(0.6),
		calm:    -5, // perfect fifth above the low drone
		palette: dungeonPalette,
		leads:   dungeonLeads,
	},
	// The bright mirror: elevation raises and opens (mixolydian -> major ->
	// lydian, higher root, denser lifted bells, breeze instead of rumble).
	{
		name:     "overworld",
		scales:   scalesByBrightness,
		bpm:      lin(74, 3),
		rootBase: 62, rootStep: 1,
		drone:    lin(0.25, 0.03),
		fifth:    lin(0.3, 0.05),
		pad:      lin(0.7, 0),
		bell:     lin(0.2, 0.04),
		air:      lin(0.15, 0.02),
		calm:     -8, // major third above the low drone: sunshine
		bellLift: 12,
		palette:  overworldPalette,
		leads:    overworldLeads,
	},
}

// MoodNames lists the moods DeriveMood knows, darkest first.
func MoodNames() []string {
	names := make([]string, len(moods))
	for i, m := range moods {
		names[i] = m.name
	}
	return names
}

// DeriveMood maps game state to music for a named mood: level says where you
// are, tension (0..1) says what is happening. Tension 0 reproduces the
// pre-tension output exactly, because it takes no extra rng draws. It reports
// false for a mood it does not know.
func DeriveMood(name string, seed uint64, level int, tension float64) (Params, bool) {
	for i := range moods {
		if moods[i].name == name {
			return moods[i].derive(seed, level, tension), true
		}
	}
	return Params{}, false
}

func (m *mood) derive(seed uint64, level int, tension float64) Params {
	r := rng{seed ^ uint64(level)*0x9e3779b97f4a7c15 | 1}
	l := float64(level)
	t := math.Max(0, math.Min(1, tension))
	// Field order matters: the BPM jitter is drawn before the root offset.
	p := Params{
		BPM:         m.bpm.at(l) + float64(r.n(5)) + float64(30*t),
		Root:        m.rootBase + m.rootStep*level + []int{-2, 0, 0, 3}[r.n(4)],
		Scale:       m.scales[min(level/2, len(m.scales)-1)],
		DroneGain:   m.drone.at(l),
		FifthGain:   m.fifth.at(l) + float64(0.25*t),
		PadDensity:  m.pad.at(l) * (1 - float64(0.6*t)),
		BellDensity: m.bell.at(l) * (1 - float64(0.5*t)),
		AirGain:     m.air.at(l) + float64(0.1*t),
		PulseGain:   t,
		FifthOffset: tenseOffset(m.calm, t),
		BellLift:    m.bellLift,
		Palette:     m.palette,
		Seed:        seed,
	}
	pickLead(&p, m.leads)
	return p
}

// DeriveDungeon is DeriveMood("dungeon", ...): deeper is darker.
func DeriveDungeon(seed uint64, depth int, tension float64) Params {
	p, _ := DeriveMood("dungeon", seed, depth, tension)
	return p
}

// DeriveOverworld is DeriveMood("overworld", ...): higher is brighter.
func DeriveOverworld(seed uint64, elevation int, tension float64) Params {
	p, _ := DeriveMood("overworld", seed, elevation, tension)
	return p
}
