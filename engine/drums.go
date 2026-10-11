package engine

// ---- drums: the rhythmic moods' lanes ----
//
// Drums run beside the six melodic tracks rather than as more of them: they
// need 16th notes, where a sequencer row is an eighth, and keeping them apart
// leaves the moods without drums untouched sample for sample.

const nDrums = 3 // kick, clap, shaker

// drumKit is each lane's patch and the note it plays at. The notes matter
// only for the pitched parts: the kick's fundamental and the clap's body.
var drumKit = [nDrums]struct {
	patch Patch
	note  int
}{{kick, 35}, {clap, 53}, {shaker, 72}}

// groove is two bars of 16th notes per drum, 32 steps, one pattern long. A
// step is a velocity or a chance:
//
//	X accent   x hit   o ghost   . rest
//	? a hit with probability DrumBusy (level fills the shaker in)
//	f a fill hit with probability 0.15 + 0.6*tension
type groove [nDrums]string

var grooveLib = map[string]groove{
	// relaxed: kick on one and three with a push into three, clap on the
	// backbeat, a lilting shaker
	"amble": {
		"X.......x.x.....X.......x.....f.",
		"....x.......x.......x.......x.ff",
		"x?o?x?o?x?o?x?o?x?o?x?o?x?o?x?o?",
	},
	// skipping: the kick syncopated against a straight backbeat
	"skip": {
		"X..x..x.....x...X..x..x.......f.",
		"....x.......x.......x.......x.f.",
		"x.o?x.o?x.o?x.o?x.o?x.o?x.o?x.o?",
	},
	// stomping: four on the floor for the festival end of a village
	"stomp": {
		"X...x...X...x...X...x...X...x.ff",
		"....X.......X.......X.......X...",
		"o?x?o?x?o?x?o?x?o?x?o?x?o?x?o?x?",
	},
}

// drumHit is a lane's two 16ths within one row: on the row, and the off 16th
// that Swing pushes late. Gains; 0 is no hit.
type drumHit struct{ on, off float64 }

// buildDrums fills one pattern of drum lanes. It draws from its own rng
// stream, so drum chances never shift what the melodic tracks play.
func buildDrums(p Params, pat int) [nDrums][rows]drumHit {
	var out [nDrums][rows]drumHit
	g, ok := grooveLib[p.Groove]
	if !ok || p.DrumGain <= 0 {
		return out
	}
	r := rng{(p.Seed ^ uint64(pat)*0x9e3779b97f4a7c15) + 0x9700e5 | 1}
	fill := 0.15 + 0.6*p.PulseGain
	for d := range g {
		for i := 0; i < 2*rows; i++ {
			var v float64
			switch g[d][i] {
			case 'X':
				v = 1
			case 'x':
				v = 0.7
			case 'o':
				v = 0.4
			case '?':
				if r.f() < p.DrumBusy {
					v = 0.45
				}
			case 'f':
				if r.f() < fill {
					v = 0.6
				}
			}
			if i%2 == 0 {
				out[d][i/2].on = v * p.DrumGain
			} else {
				out[d][i/2].off = v * p.DrumGain
			}
		}
	}
	return out
}

// stepDrums plays the drums' on-row hits and arms the off 16ths.
func (s *Synth) stepDrums() {
	for d := range s.dvoices {
		v := &s.dvoices[d]
		h := s.drums[d][s.row]
		if h.on > 0 {
			v.strike(d, h.on, s.drumSeed(d))
		} else if v.active && v.gate {
			v.gate = false
		}
		s.offLeft[d] = 0
		if h.off > 0 {
			// the off 16th lands Swing of a row past halfway: 0 is straight,
			// 1/6 a triplet shuffle
			s.offLeft[d] = max(1, int(float64(s.samplesPerRow)*(0.5+s.params.Swing)))
			s.offGain[d] = h.off
			// seeded now: by the time it fires, step has moved on to the next row
			s.offSeed[d] = s.drumSeed(d) + 1<<32
		}
	}
}

// drumSeed seeds lane d's hit on the current row.
func (s *Synth) drumSeed(d int) uint64 {
	return s.params.Seed + uint64(s.pat*rows+s.row)*nDrums + uint64(d)
}

// strike hits drum lane d afresh. trigger's legato path suits drones but not
// drums: a lane that hits every 16th is still active at its next hit, and
// carrying its envelope and phase over would soften every transient and
// start the clap's re-hits mid-burst.
func (v *Voice) strike(d int, gain float64, seed uint64) {
	v.active = false
	v.trigger(drumKit[d].patch, drumKit[d].note, 1, gain, seed)
}

// runDrums fires any off 16th that is due and renders the drum voices.
func (s *Synth) runDrums(l, r *float64) {
	for d := range s.dvoices {
		v := &s.dvoices[d]
		if s.offLeft[d] > 0 {
			s.offLeft[d]--
			if s.offLeft[d] == 0 {
				v.strike(d, s.offGain[d], s.offSeed[d])
			}
		}
		if v.active {
			v.run(l, r, &s.dbus)
		}
	}
}

// pickGroove draws the groove from its own stream, as pickLead draws the
// lead, so a mood's grooves never change its notes.
func pickGroove(p *Params, names []string) {
	r := rng{p.Seed*0x9e3779b97f4a7c15 + 0x9700e5 | 1}
	p.Groove = names[r.n(len(names))]
}
