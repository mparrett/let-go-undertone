package engine

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// Every groove is three lanes of two bars of 16ths, in the step alphabet.
func TestGroovesAreWellFormed(t *testing.T) {
	for name, g := range grooveLib {
		for d, lane := range g {
			if len(lane) != 2*rows {
				t.Errorf("%s lane %d: %d steps, want %d", name, d, len(lane), 2*rows)
			}
			if i := strings.IndexFunc(lane, func(c rune) bool { return !strings.ContainsRune("Xxo.?f", c) }); i >= 0 {
				t.Errorf("%s lane %d: step %d is %q", name, d, i, lane[i])
			}
		}
	}
	for _, m := range moods {
		for _, g := range m.grooves {
			if _, ok := grooveLib[g]; !ok {
				t.Errorf("mood %s names groove %q, which is not in grooveLib", m.name, g)
			}
		}
	}
}

// Drums belong to the moods that ask for them; the rest stay silent.
func TestDrumsOnlyInRhythmicMoods(t *testing.T) {
	for _, name := range MoodNames() {
		for level := 0; level <= 5; level++ {
			p, _ := DeriveMood(name, 1, level, 0.5)
			hits := 0
			for _, lane := range buildDrums(p, 0) {
				for _, h := range lane {
					if h.on > 0 || h.off > 0 {
						hits++
					}
				}
			}
			if rhythmic := name == "village"; rhythmic != (hits > 0) {
				t.Errorf("%s level %d: %d drum hits", name, level, hits)
			}
		}
	}
}

// The same seed and pattern give the same drums; fills make patterns differ.
func TestGroovesAreDeterministicAndVary(t *testing.T) {
	p, _ := DeriveMood("village", 2, 3, 1)
	if buildDrums(p, 5) != buildDrums(p, 5) {
		t.Fatal("same params and pattern built different drums")
	}
	differ := false
	for pat := 1; pat < 8 && !differ; pat++ {
		differ = buildDrums(p, pat) != buildDrums(p, 0)
	}
	if !differ {
		t.Fatal("eight patterns at full tension all played identical drums")
	}
}

// Level fills the shaker in: more ? steps play higher up.
func TestLevelBusiesTheShaker(t *testing.T) {
	count := func(level int) int {
		p, _ := DeriveMood("village", 2, level, 0)
		n := 0
		for pat := 0; pat < 16; pat++ {
			for _, h := range buildDrums(p, pat)[2] {
				if h.on > 0 {
					n++
				}
				if h.off > 0 {
					n++
				}
			}
		}
		return n
	}
	if lo, hi := count(0), count(5); hi <= lo {
		t.Fatalf("shaker hits at level 5 (%d) not above level 0 (%d)", hi, lo)
	}
}

// soloLevel plays one drum on every beat for two seconds and returns its rms
// while sounding, in dB.
func soloLevel(d int) float64 {
	var v Voice
	var sum float64
	n := 0
	beat := SampleRate / 2
	for i := 0; i < 2*SampleRate; i++ {
		if i%beat == 0 {
			v.trigger(drumKit[d].patch, drumKit[d].note, 1, 1, uint64(i))
		}
		if i%beat == beat/4 {
			v.gate = false
		}
		if v.active {
			var l, r, dl float64
			v.run(&l, &r, &dl)
			if x := (l + r) / 2; math.Abs(x) > 1e-3 {
				sum += x * x
				n++
			}
		}
	}
	return 10 * math.Log10(sum/float64(n))
}

// The kit's balance, set by ear and by a measured review: the clap within
// 6 dB of the kick, the shaker 6 to 18 dB under it, as hats sit.
func TestKitBalance(t *testing.T) {
	kickL, clapL, shakerL := soloLevel(0), soloLevel(1), soloLevel(2)
	if math.Abs(kickL-clapL) > 6 {
		t.Errorf("clap %.1f dB against kick %.1f dB", clapL, kickL)
	}
	if gap := kickL - shakerL; gap < 6 || gap > 18 {
		t.Errorf("shaker %.1f dB, %.1f dB under the kick", shakerL, gap)
	}
}

// Drums bring the engine's first sharp transients; the loudest village
// scenes must keep headroom under the master's saturation.
func TestVillagePeaks(t *testing.T) {
	for _, c := range []struct {
		seed    uint64
		level   int
		tension float64
	}{{7, 5, 0}, {2, 5, 1}, {11, 2, 0.8}} {
		p, _ := DeriveMood("village", c.seed, c.level, c.tension)
		s := NewSynth(p)
		buf := make([]byte, SampleRate*4*12)
		s.Read(buf)
		peak := 0.0
		for i := 0; i+1 < len(buf); i += 2 {
			peak = max(peak, math.Abs(float64(int16(uint16(buf[i])|uint16(buf[i+1])<<8))))
		}
		if db := 20 * math.Log10(peak/32768); db > -2 {
			t.Errorf("village %+v peaks at %.1f dBFS, want at most -2", c, db)
		}
	}
}

// The drums draw from their own rng streams and voices, so a groove never
// changes what the melodic tracks play: rendered with and without drums, the
// melodic voices hold the same state throughout.
func TestGrooveLeavesMelodyAlone(t *testing.T) {
	p, _ := DeriveMood("village", 7, 3, 0.6)
	quiet := p
	quiet.Groove, quiet.DrumGain, quiet.DrumBusy, quiet.Swing = "", 0, 0, 0
	a, b := NewSynth(p), NewSynth(quiet)
	buf := make([]byte, 4*a.samplesPerRow/3)
	for i := 0; i < 3*rows*4; i++ { // four patterns, in third-of-a-row chunks
		a.Read(buf)
		b.Read(buf)
		if fmt.Sprint(a.voices) != fmt.Sprint(b.voices) {
			t.Fatalf("chunk %d (pattern %d row %d): the groove changed the melodic voices", i, a.pat, a.row)
		}
	}
}

// Under a grid the drums are part of the bed: turn the bed off and they stop.
func TestGridBedCarriesDrums(t *testing.T) {
	p, _ := DeriveMood("village", 2, 3, 0)
	s := NewSynth(p)
	s.SetGrid(&Grid{Bed: false})
	s.Read(make([]byte, SampleRate*4*3))
	for d, v := range s.dvoices {
		if v.active {
			t.Errorf("drum lane %d sounds with the bed off", d)
		}
	}
}

// A drum hit while the lane still sounds starts a fresh transient: no
// envelope level or oscillator phase carried over from the last hit.
func TestDrumStrikeIsFresh(t *testing.T) {
	for d := range nDrums {
		var v Voice
		v.strike(d, 1, 1)
		var l, r float64
		var bus float64
		for range SampleRate / 50 {
			v.run(&l, &r, &bus)
		}
		if !v.active {
			t.Fatalf("lane %d went quiet within 20 ms; the test needs it sounding", d)
		}
		v.strike(d, 1, 2)
		for i, u := range v.units {
			if u.env != 0 || u.phase != 0 {
				t.Errorf("lane %d unit %d: re-hit kept env %.3g phase %.3g", d, i, u.env, u.phase)
			}
		}
	}
}

// SetParametersNow switches the drums on the next row, as it does swing,
// rather than leaving the old groove playing out its pattern.
func TestSetParametersNowSwitchesDrums(t *testing.T) {
	village, _ := DeriveMood("village", 7, 5, 0)
	aurora, _ := DeriveMood("aurora", 7, 1, 0)
	hits := func(s *Synth) bool {
		for d := range s.drums {
			for _, h := range s.drums[d] {
				if h.on > 0 || h.off > 0 {
					return true
				}
			}
		}
		return false
	}
	for _, c := range []struct {
		name     string
		from, to Params
		want     bool
	}{{"village -> aurora", village, aurora, false}, {"aurora -> village", aurora, village, true}} {
		s := NewSynth(c.from)
		s.Read(make([]byte, 4*s.samplesPerRow*3))
		s.SetParametersNow(c.to)
		s.Read(make([]byte, 4*s.samplesPerRow))
		if s.pat != 0 {
			t.Fatalf("crossed into pattern %d; the test needs the switch mid-pattern", s.pat)
		}
		if hits(s) != c.want {
			t.Errorf("%s mid-pattern: drums sounding = %v, want %v", c.name, !c.want, c.want)
		}
	}
}
