package engine

import (
	"encoding/binary"
	"math"
	"testing"
)

// rms of one stereo s16 buffer, both channels.
func rms(b []byte) float64 {
	var sum float64
	for i := 0; i+1 < len(b); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(b[i:]))) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(b)/2))
}

func patternBytes(p Params) int { return int(SampleRate*60/(p.BPM*2)) * Steps * 4 }

func TestGridWithoutBedIsOnlyTheGrid(t *testing.T) {
	p := DeriveOverworld(1, 2, 0)
	s := NewSynth(p)
	s.SetGrid(&Grid{}) // empty, no bed
	buf := make([]byte, patternBytes(p))
	s.Read(buf) // first pattern: delay tails and releases die away
	s.Read(buf)
	if v := rms(buf); v > 1e-3 {
		t.Errorf("an empty grid with the bed off should be silent by the second pattern, rms %.5f", v)
	}

	g := Grid{}
	g.Lead[0], g.Bass[4], g.Pulse[8] = 5, 1, true
	s.SetGrid(&g)
	s.Read(buf)
	if v := rms(buf); v < 1e-3 {
		t.Errorf("a grid with notes should sound, rms %.5f", v)
	}
}

func TestGridEditsLandAtTheNextRow(t *testing.T) {
	p := DeriveOverworld(1, 2, 0)
	s := NewSynth(p)
	s.SetGrid(&Grid{})
	row := int(SampleRate * 60 / (p.BPM * 2))
	buf := make([]byte, row*4)
	for i := 0; i < 2*Steps; i++ { // two patterns of silence
		s.Read(buf)
	}
	g := Grid{}
	g.Lead[1] = 3 // the row after the one about to play
	s.SetGrid(&g)
	s.Read(buf) // row 0: still silent
	if v := rms(buf); v > 1e-3 {
		t.Fatalf("row 0 sounded before its note, rms %.5f", v)
	}
	s.Read(buf) // row 1: the edit
	if v := rms(buf); v < 1e-3 {
		t.Errorf("an edit to the next row should sound within the pattern, rms %.5f", v)
	}
}

func TestRowAtFollowsRenderedRows(t *testing.T) {
	p := DeriveDungeon(1, 1, 0)
	s := NewSynth(p)
	row := int64(SampleRate * 60 / (p.BPM * 2))
	buf := make([]byte, int(row)*20*4) // 20 rows: wraps into the next pattern
	s.Read(buf)
	if got := s.Frames(); got != row*20 {
		t.Fatalf("Frames() = %d, want %d", got, row*20)
	}
	for _, c := range []struct {
		frame int64
		want  int
	}{{0, 0}, {row - 1, 0}, {row, 1}, {row*15 + 3, 15}, {row * 16, 0}, {row*19 + 1, 3}} {
		if got := s.RowAt(c.frame); got != c.want {
			t.Errorf("RowAt(%d) = %d, want %d", c.frame, got, c.want)
		}
	}
}

func TestSeedGridIsDeterministicAndInRange(t *testing.T) {
	p := DeriveOverworld(7, 3, 0)
	a, b := SeedGrid(p), SeedGrid(p)
	if a != b {
		t.Fatal("same params, different grids")
	}
	notes := 0
	for i := 0; i < Steps; i++ {
		for _, d := range []int{a.Lead[i], a.Bass[i]} {
			if d < 0 || d > Degrees {
				t.Fatalf("step %d: degree %d out of range", i, d)
			}
		}
		if a.Lead[i] > 0 {
			notes++
		}
	}
	if notes < 4 {
		t.Errorf("seeded lead has %d notes; too sparse to start from", notes)
	}
	p.Seed = 8
	if SeedGrid(p) == a {
		t.Error("a different seed gave the same grid")
	}
}
