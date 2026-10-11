package main

import "testing"

func TestSignedDepthKeepsAnExplicitMood(t *testing.T) {
	for _, c := range []struct {
		mood      string
		depth     int
		wantMood  string
		wantLevel int
	}{
		{"dungeon", -3, "overworld", 3},
		{"", -2, "overworld", 2},
		{"dungeon", 4, "dungeon", 4},
		{"aurora", -2, "aurora", -2},
		{"meadow", -3, "meadow", -3},
	} {
		m, l := signedDepth(c.mood, c.depth)
		if m != c.wantMood || l != c.wantLevel {
			t.Errorf("signedDepth(%q, %d) = %q, %d; want %q, %d", c.mood, c.depth, m, l, c.wantMood, c.wantLevel)
		}
	}
}
