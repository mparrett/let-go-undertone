package main

import "testing"

func TestTensionZeroIsInert(t *testing.T) {
	p := DeriveDungeon(1, 5, 0)
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
	calm := DeriveDungeon(1, 5, 0)
	hot := DeriveDungeon(1, 5, 1)
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

func TestOverworldIsBrighter(t *testing.T) {
	dun := DeriveDungeon(1, 3, 0)
	over := DeriveOverworld(1, 3, 0)
	if over.Root <= dun.Root {
		t.Fatalf("overworld root should sit higher: %d <= %d", over.Root, dun.Root)
	}
	if over.FifthOffset != -8 {
		t.Fatalf("calm overworld wants the major third, got %d", over.FifthOffset)
	}
	if over.BellDensity <= dun.BellDensity || over.BellLift != 12 {
		t.Fatalf("overworld bells should be denser and lifted: %v vs %v, lift %d",
			over.BellDensity, dun.BellDensity, over.BellLift)
	}
	if over.DroneGain >= dun.DroneGain {
		t.Fatalf("overworld drone is a bed, not a weight: %v >= %v", over.DroneGain, dun.DroneGain)
	}
	if DeriveOverworld(1, 3, 1).FifthOffset != -11 {
		t.Fatal("overworld tension should reach the same dread interval")
	}
}

func TestLeadLibrary(t *testing.T) {
	p := DeriveDungeon(1, 3, 0)
	if _, ok := leadLib[p.LeadName]; !ok {
		t.Fatalf("derive picked an unknown lead %q", p.LeadName)
	}
	applyLead(&p, "reed")
	if p.LeadName != "reed" || p.LeadGate != 3 {
		t.Fatalf("override failed: %q gate %d", p.LeadName, p.LeadGate)
	}
	seen := map[string]bool{}
	for seed := uint64(1); seed <= 12; seed++ {
		seen[DeriveDungeon(seed, 3, 0).LeadName] = true
	}
	if len(seen) < 3 {
		t.Fatalf("12 seeds should audition at least 3 leads, got %v", seen)
	}
}

func TestSetParametersAppliesAtPatternBoundary(t *testing.T) {
	s := NewSynth(DeriveDungeon(1, 3, 0))
	buf := make([]byte, s.samplesPerRow*4*4) // advance 4 rows into pattern 0
	s.Read(buf)
	before := s.params.BPM
	next := DeriveDungeon(1, 3, 1)
	s.SetParameters(next)
	if s.params.BPM != before {
		t.Fatal("staged parameters must not apply mid-pattern")
	}
	rest := make([]byte, (s.samplesPerRow*(rows-4)+s.samplesPerRow)*4)
	s.Read(rest) // cross the pattern boundary
	if s.params.BPM != next.BPM {
		t.Fatalf("staged parameters should apply at the boundary: got bpm %v, want %v",
			s.params.BPM, next.BPM)
	}
}
