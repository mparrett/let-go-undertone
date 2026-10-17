package engine

import (
	"math"
	"testing"
)

// An impulse into the reverb rings as a decorrelated stereo tail that is
// still sounding after half a second and gone within ten.
func TestReverbTailDecays(t *testing.T) {
	v := newReverb()
	peak := func(from, to int, in float64) (pl, pr, diff float64) {
		for i := from; i < to; i++ {
			l, r := v.process(in)
			in = 0
			if math.IsNaN(l) || math.IsInf(l, 0) {
				t.Fatalf("sample %d: %v", i, l)
			}
			pl, pr = max(pl, math.Abs(l)), max(pr, math.Abs(r))
			diff = max(diff, math.Abs(l-r))
		}
		return
	}
	early, _, diff := peak(0, SampleRate/2, 1)
	late, _, _ := peak(SampleRate/2, SampleRate, 0)
	peak(SampleRate, 10*SampleRate, 0) // let it ring out
	gone, _, _ := peak(10*SampleRate, 11*SampleRate, 0)
	if late < 1e-3 || late > early || gone > 1e-5 || diff < 1e-3 {
		t.Errorf("peaks %.2g in the first half second, %.2g in the next, %.2g after ten; L-R differ by %.2g", early, late, gone, diff)
	}
}

// A synth whose patches never send to the reverb never runs it.
func TestReverbStaysOffWithoutSends(t *testing.T) {
	s := NewSynth(DeriveDungeon(1, 3, 0.5))
	render(s, 2*SampleRate)
	if s.verb.on {
		t.Error("dungeon has no verb sends, yet the reverb is running")
	}
	p, _ := DeriveMood("rust", 1, 0, 0)
	s = NewSynth(p)
	render(s, 2*SampleRate)
	if !s.verb.on {
		t.Error("rust sends to the reverb, yet it is off")
	}
}
