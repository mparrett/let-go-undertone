package letgo

import (
	"testing"

	"github.com/nooga/let-go/pkg/api"

	"github.com/mparrett/undertone/engine"
)

// Importing the package is the whole install for an lg runtime that links it
// as a plain Go dep: no SetMusic, no generated interop. If init stopped
// registering, lg code would fail to resolve audio/* with nothing pointing at
// the cause.
func TestNamespaceRegisteredAtImport(t *testing.T) {
	c, err := api.NewLetGo("importtest")
	if err != nil {
		t.Fatal(err)
	}
	if CurrentMusic() == nil {
		t.Fatal("no default Music; natives would dereference nil")
	}
	if _, err := c.Run("(audio/set-parameters {:bpm 77.0})"); err != nil {
		t.Fatalf("audio namespace not registered at import: %v", err)
	}
}

// LoadEmbedded is the browser's whole load lane — GOOS=js has no filesystem,
// so if this breaks, a wasm host dies at boot. Proving it natively keeps the
// failure findable by `go test`.
func TestLoadEmbeddedRunsTheWholeLane(t *testing.T) {
	m := NewMusic(engine.DeriveDungeon(1, 0, 0))
	SetMusic(m)
	c, err := api.NewLetGo("embedtest")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately no SetLoadPath: resolution must come from the embedded
	// sources alone, exactly as in the browser.
	if err := LoadEmbedded(c); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run("(ambience/score! 7 4 0.6)"); err != nil {
		t.Fatal(err)
	}
	if got, want := m.Params().BPM, engine.DeriveDungeon(7, 4, 0.6).BPM; got != want {
		t.Errorf("bpm after embedded ambience/score! = %v, want %v", got, want)
	}
}

// The seam itself: lg trigger maps must reach the engine and make sound. The
// synth is set directly (no oto, no sound card) over silent parameters, so
// any PCM that appears is the one-shot lane's.
func TestAudioTriggerReachesTheEngine(t *testing.T) {
	m := NewMusic(engine.DeriveDungeon(1, 0, 0))
	SetMusic(m)
	c, err := api.NewLetGo("triggertest")
	if err != nil {
		t.Fatal(err)
	}

	// Write-only contract: with no synth (music not started), triggers drop
	// without error.
	if _, err := c.Run(`(audio/trigger {:shot :hit :gain 1.0})`); err != nil {
		t.Fatalf("trigger with no synth errored: %v", err)
	}

	silent := engine.Params{BPM: 120, Seed: 1, Scale: []int{0, 2, 3, 5, 7, 9, 10}}
	m.mu.Lock()
	m.synth = engine.NewSynth(silent)
	m.mu.Unlock()
	if _, err := c.Run(`(audio/trigger [{:shot :hit :gain 1.0 :pan 0.3}
	                                    {:shot :pluck :degree 2 :octave 1 :seed "42"}])`); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, engine.SampleRate*4)
	m.synth.Read(buf)
	for _, b := range buf {
		if b != 0 {
			return
		}
	}
	t.Error("triggers sent from lg rendered pure silence; the map -> Shot plumbing dropped them")
}
