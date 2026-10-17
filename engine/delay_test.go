package engine

import "testing"

// The delay stays three rows long as the tempo changes, so its echoes land
// on the grid at any tension; a tempo no slower than one it has played
// reuses the buffer it already has.
func TestDelayFollowsTheTempo(t *testing.T) {
	p := DeriveDungeon(1, 0, 0)
	s := NewSynth(p)
	var slowest *float64
	for _, bpm := range []float64{p.BPM + 30, p.BPM - 10, p.BPM, p.BPM + 30} {
		q := p
		q.BPM = bpm
		s.SetParametersNow(q)
		render(s, s.samplesPerRow+1) // past the next row, where it applies
		if want := 3 * int(SampleRate*60/(bpm*2)); len(s.dl) != want || len(s.dr) != want {
			t.Errorf("at %v bpm the delay is %d samples, want %d", bpm, len(s.dl), want)
		}
		if bpm == p.BPM-10 {
			slowest = &s.dl[0]
		} else if slowest != nil && &s.dl[0] != slowest {
			t.Errorf("at %v bpm the delay reallocated", bpm)
		}
	}
}
