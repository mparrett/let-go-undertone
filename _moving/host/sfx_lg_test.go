package host

import (
	"fmt"
	"testing"

	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/vm"

	"undertone/engine"
)

// These tests are about the sfx VOCABULARY — the pure descriptor->trigger
// derivation — plus the one seam below it, audio/trigger. The engine's own
// behaviour (pan, delay, voice death) is pinned in engine/sfx_test.go; here
// the question is whether the lg layer says the right things to it.

func sfxRuntime(t *testing.T) (*api.LetGo, *Music) {
	t.Helper()
	m := NewMusic(engine.DeriveDungeon(1, 0, 0))
	Install(m)
	c, err := api.NewLetGo("sfxtest")
	if err != nil {
		t.Fatal(err)
	}
	c.SetLoadPath([]string{"../lg"})
	if _, err := c.Run("(require 'sfx)"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run(`(def w {:seed-input 42 :depth 3 :fov #{}
	                           :entities {:player {:id :player :body {:hp 20 :max-hp 20}}}})`); err != nil {
		t.Fatal(err)
	}
	return c, m
}

func evalStr(t *testing.T, c *api.LetGo, expr string) string {
	t.Helper()
	v, err := c.Run(expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	s, ok := v.(vm.String)
	if !ok {
		t.Fatalf("%s: got %T, want a string", expr, v)
	}
	return string(s)
}

func evalF(t *testing.T, c *api.LetGo, expr string) float64 {
	t.Helper()
	v, err := c.Run(expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	switch n := v.(type) {
	case vm.Float:
		return float64(n)
	case vm.Int:
		return float64(n)
	}
	t.Fatalf("%s: got %T (%v), want a number", expr, v, v)
	return 0
}

// The mix hierarchy is a design property, not a tuning accident: a footstep
// plays every turn and must sit under a hit, and distance must pull any
// sound down without ever muting it outright.
func TestGainHierarchyAndAttenuation(t *testing.T) {
	c, _ := sfxRuntime(t)

	step := evalF(t, c, `(:gain (first (sfx/for-events w 1 [{:kind :footstep}])))`)
	hit := evalF(t, c, `(:gain (first (sfx/for-events w 1 [{:kind :combat-hit}])))`)
	if step >= hit {
		t.Errorf("footstep gain %v >= hit gain %v; the every-turn sound must be the quiet one", step, hit)
	}

	near := evalF(t, c, `(:gain (first (sfx/for-events w 1 [{:kind :combat-hit :dist 0}])))`)
	far := evalF(t, c, `(:gain (first (sfx/for-events w 1 [{:kind :combat-hit :dist 6}])))`)
	if far >= near {
		t.Errorf("gain at dist 6 (%v) >= at dist 0 (%v); distance must attenuate", far, near)
	}
	if far <= 0 {
		t.Errorf("gain at dist 6 is %v; attenuation approaches zero, the cutoff is the producer's call", far)
	}
}

func TestPanFollowsDirection(t *testing.T) {
	c, _ := sfxRuntime(t)
	left := evalF(t, c, `(:pan (first (sfx/for-events w 1 [{:kind :footstep :dist 4 :dir -1}])))`)
	center := evalF(t, c, `(:pan (first (sfx/for-events w 1 [{:kind :footstep}])))`)
	right := evalF(t, c, `(:pan (first (sfx/for-events w 1 [{:kind :footstep :dist 4 :dir 1}])))`)
	if !(left < center && center < right) {
		t.Errorf("pan left/center/right = %v/%v/%v, want strictly increasing", left, center, right)
	}
	if left <= 0.05 || right >= 0.95 {
		t.Errorf("pan reached the hard edges (%v, %v); a fully one-eared sound reads as a glitch", left, right)
	}
}

// The pickup chime's whole trick: its notes come from the same derivation the
// music uses, so they are in the floor's scale by construction.
func TestPickupNotesAreInTheFloorsScale(t *testing.T) {
	c, _ := sfxRuntime(t)
	if got := evalF(t, c, `(count (sfx/for-events w 1 [{:kind :pickup}]))`); got != 2 {
		t.Fatalf("pickup produced %v shots, want a two-note dyad", got)
	}
	inScale := evalStr(t, c, `
	  (let [p     (music/params-for w 0.0)
	        pcs   (set (map (fn [iv] (mod (+ (:root p) iv) 12)) (:scale p)))
	        shots (sfx/for-events w 1 [{:kind :pickup}])]
	    (if (every? (fn [s] (contains? pcs (mod (:note s) 12))) shots) "yes" "no"))`)
	if inScale != "yes" {
		t.Error("a pickup note fell outside the floor's scale; the dyad must be in key with the score by construction")
	}
	delay := evalF(t, c, `(:delay-ms (second (sfx/for-events w 1 [{:kind :pickup}])) 0)`)
	if delay <= 0 {
		t.Errorf("second pickup note has delay %v; the arpeggio staggering is the pickup gesture", delay)
	}
}

// Same turn, same events -> the same triggers (replay-stable); a different
// turn salt must vary the jitter so consecutive footsteps differ.
func TestTriggersAreSaltedButDeterministic(t *testing.T) {
	c, _ := sfxRuntime(t)
	a := evalStr(t, c, `(pr-str (sfx/for-events w 7 [{:kind :footstep}]))`)
	b := evalStr(t, c, `(pr-str (sfx/for-events w 7 [{:kind :footstep}]))`)
	if a != b {
		t.Errorf("same world, same salt, different triggers:\n%s\n%s", a, b)
	}
	seeds := map[string]bool{}
	for salt := 0; salt < 6; salt++ {
		seeds[evalStr(t, c, fmt.Sprintf(
			`(pr-str (:seed (first (sfx/for-events w %d [{:kind :footstep}]))))`, salt))] = true
	}
	if len(seeds) < 6 {
		t.Errorf("6 salts produced only %d distinct seeds; consecutive footsteps would share jitter", len(seeds))
	}
}

func TestUnknownKindYieldsNoShots(t *testing.T) {
	c, _ := sfxRuntime(t)
	if got := evalF(t, c, `(count (sfx/for-events w 1 [{:kind :thunderclap}]))`); got != 0 {
		t.Errorf("unknown kind produced %v shots; producers may emit kinds a consumer does not voice", got)
	}
}

// The seam itself: lg trigger maps must reach the engine and make sound. The
// synth is set directly (no oto, no sound card) over silent parameters, so
// any PCM that appears is the one-shot lane's.
func TestAudioTriggerReachesTheEngine(t *testing.T) {
	c, m := sfxRuntime(t)
	silent := engine.Params{BPM: 120, Seed: 1, Scale: []int{0, 2, 3, 5, 7, 9, 10}}

	// Write-only contract: with no synth (music not started), triggers drop
	// without error — the -dry demo depends on this.
	if _, err := c.Run(`(audio/trigger (sfx/for-events w 1 [{:kind :combat-hit}]))`); err != nil {
		t.Fatalf("trigger with no synth errored: %v", err)
	}

	m.mu.Lock()
	m.synth = engine.NewSynth(silent)
	m.mu.Unlock()
	if _, err := c.Run(`(audio/trigger (sfx/for-events w 1 [{:kind :combat-hit} {:kind :pickup}]))`); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, engine.SampleRate*4)
	m.synth.Read(buf)
	silentPCM := true
	for _, b := range buf {
		if b != 0 {
			silentPCM = false
			break
		}
	}
	if silentPCM {
		t.Error("triggers sent from lg rendered pure silence; the map -> Trigger plumbing dropped them")
	}
}
