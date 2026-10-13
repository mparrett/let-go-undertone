package engine

import (
	"reflect"
	"strings"
	"testing"
)

// A built-in mood taken through MoodSpec derives exactly what DeriveMood
// does, so a designer starting from one hears it unchanged.
func TestMoodSpecRoundTrip(t *testing.T) {
	for _, name := range MoodNames() {
		s, ok := MoodByName(name)
		if !ok || s.Palette == "" {
			t.Fatalf("%s: spec %v, palette %q", name, ok, s.Palette)
		}
		for _, seed := range []uint64{1, 7, 42} {
			for _, level := range []int{-2, 0, 1, 3, 8} {
				for _, tension := range []float64{0, 0.5, 1} {
					want, _ := DeriveMood(name, seed, level, tension)
					got, err := s.Derive(seed, level, tension)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s seed %d level %d tension %v: spec derives differently (%v)", name, seed, level, tension, err)
					}
				}
			}
		}
	}
}

func TestMoodSpecRejectsWhatTheEngineLacks(t *testing.T) {
	base, _ := MoodByName("village")
	for _, c := range []struct {
		name string
		edit func(*MoodSpec)
	}{
		{"palette", func(s *MoodSpec) { s.Palette = "nope" }},
		{"lead", func(s *MoodSpec) { s.Leads = []string{"theremin"} }},
		{"groove", func(s *MoodSpec) { s.Grooves = []string{"polka"} }},
		{"scale", func(s *MoodSpec) { s.Scales = [][]int{{0, 2, 4, 7, 9}} }},
	} {
		s := base
		c.edit(&s)
		if _, err := s.Derive(1, 0, 0); err == nil {
			t.Errorf("a spec with a bad %s derived without error", c.name)
		}
	}
}

func TestMoodSpecEDN(t *testing.T) {
	s, _ := MoodByName("village")
	e := s.EDN()
	for _, want := range []string{":village\n", ":bpm       {:base 100.0 :step 2.0 :hi 112.0}", ":grooves   [:amble :amble :skip :stomp]", ":swing     0.1}"} {
		if !strings.Contains(e, want) {
			t.Errorf("EDN lacks %q:\n%s", want, e)
		}
	}
	a, _ := MoodByName("aurora")
	if strings.Contains(a.EDN(), ":grooves") {
		t.Error("a mood without drums exports drum keys")
	}
}
