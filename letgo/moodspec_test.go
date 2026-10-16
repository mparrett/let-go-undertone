package letgo

import (
	"fmt"
	"testing"

	"github.com/mparrett/undertone/engine"
)

// A designer's export pastes into ambience.lg: each built-in mood's EDN,
// read by lg, equals that mood's entry in the lg table (bar :palette, which
// the lg side does not carry yet).
func TestMoodSpecEDNMatchesLgTable(t *testing.T) {
	c, _ := lgRuntime(t)
	for _, name := range engine.MoodNames() {
		s, _ := engine.MoodByName(name)
		v, err := c.Run("(= (dissoc (get {" + s.EDN() + "} :" + name + ") :palette) (get ambience/moods :" + name + "))")
		if err != nil {
			t.Fatalf("%s: lg could not read the export: %v\n%s", name, err, s.EDN())
		}
		if v.String() != "true" {
			t.Errorf("%s: the export differs from ambience.lg's entry:\n%s", name, s.EDN())
		}
	}
}

// An export under a name the host has never seen still plays as designed:
// derive-spec takes the entry itself, and its :palette picks the patches that
// the host would otherwise pick by mood name.
func TestDeriveSpecMatchesMoodSpec(t *testing.T) {
	c, m := lgRuntime(t)
	s, _ := engine.MoodByName("village")
	s.Name, s.Palette = "cavern", "aurora"
	s.BPM.Base -= 9
	s.Grooves = engine.GrooveNames()[:2]
	if err := c.Def("cavern-src", "{"+s.EDN()+"}"); err != nil {
		t.Fatal(err)
	}
	for _, seed := range []uint64{1, 7, 42} {
		for _, level := range []int{0, 3, 8} {
			for _, tension := range []float64{0, 0.6} {
				want, err := s.Derive(seed, level, tension)
				if err != nil {
					t.Fatal(err)
				}
				name := fmt.Sprintf("cavern/seed%d/level%d/tension%.1f", seed, level, tension)
				m.params = engine.DeriveDungeon(1, 0, 0)
				expr := fmt.Sprintf("(ambience/score-spec! (:cavern (read-string cavern-src)) %d %d %v)", seed, level, tension)
				if _, err := c.Run(expr); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				checkParams(t, name, m.params, want)
			}
		}
	}
}
