package letgo

import (
	"testing"

	"github.com/mparrett/let-go-undertone/engine"
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
