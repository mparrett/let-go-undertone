package main

import "testing"

func TestTensionZeroIsInert(t *testing.T) {
	p := DeriveParams(1, 5, 0)
	if p.PulseGain != 0 {
		t.Fatalf("tension 0 must silence the pulse track, got gain %v", p.PulseGain)
	}
	if p.FifthOffset != -5 {
		t.Fatalf("tension 0 must keep the perfect fifth, got offset %d", p.FifthOffset)
	}
	pats := buildPatterns(p, 0)
	for row, tr := range pats[5] {
		if tr.note != 0 {
			t.Fatalf("tension 0 placed a pulse note at row %d", row)
		}
	}
}

func TestTensionOneAgitates(t *testing.T) {
	calm := DeriveParams(1, 5, 0)
	hot := DeriveParams(1, 5, 1)
	if hot.BPM <= calm.BPM {
		t.Fatalf("tension must raise tempo: %v <= %v", hot.BPM, calm.BPM)
	}
	if hot.FifthOffset != -11 {
		t.Fatalf("full tension should reach the minor-ninth offset, got %d", hot.FifthOffset)
	}
	pats := buildPatterns(hot, 0)
	pulses := 0
	for _, tr := range pats[5] {
		if tr.note != 0 {
			pulses++
		}
	}
	if pulses < 2 {
		t.Fatalf("full tension should place at least the 0/8 heartbeat, got %d notes", pulses)
	}
	if hot.BellDensity >= calm.BellDensity || hot.PadDensity >= calm.PadDensity {
		t.Fatalf("tension should thin the calm layers: bells %v>=%v or pads %v>=%v",
			hot.BellDensity, calm.BellDensity, hot.PadDensity, calm.PadDensity)
	}
}
