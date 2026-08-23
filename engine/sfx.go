// One-shot sfx: a small voice lane beside the sequencer.
//
// The sequencer's loops never see these voices — step() is bounded by nTracks
// and the shots live in their own array — so a one-shot can never perturb the
// music's rng streams or pattern content. That property is by construction,
// and TestGainZeroShotLeavesMusicByteIdentical pins it.
//
// Panning is per voice, applied outside the patch: the voice renders through
// its patch (authored center), the stereo pair is mono-summed, and the same
// linear law opOut uses splits it. That keeps pan out of the per-sample unit
// VM entirely.
package engine

import (
	"sync"
	"sync/atomic"
)

// ---- one-shot patches ----
// Same recipe book as the leads: filtered noise reads as impact, a swept sine
// as weight. Quiet is a design property of the footstep, not a mix decision —
// it plays every turn.

// footstep: a dull noise tick over a faint low knock. Two lowpass poles so it
// reads as boot-on-stone, not snare.
var footstep = Patch{
	{opEnv, [4]float64{0.001, 0.045, 0, 0.025}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{600, 0, 0, 0}},
	{opLpf, [4]float64{600, 0, 0, 0}},
	{opOut, [4]float64{0.9, 0.5}},
	{opEnv, [4]float64{0.001, 0.06, 0, 0.03}},
	{opOsc, [4]float64{-12, shpSine, 5, 0.02}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.5, 0.5}},
}

// hit: the classic impact — a sine thump falling fast from above, plus a
// bright noise crack. A small send puts it in the same room as the music.
var hit = Patch{
	{opEnv, [4]float64{0.001, 0.11, 0, 0.05}},
	{opOsc, [4]float64{0, shpSine, 26, 0.03}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.15, 0, 0, 0}},
	{opOut, [4]float64{0.8, 0.5}},
	{opEnv, [4]float64{0.001, 0.045, 0, 0.03}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{2800, 0, 0, 0}},
	{opOut, [4]float64{0.5, 0.5}},
}

// thud: damage you TAKE. The deliberate inverse of hit — no bright crack, a
// slower body, and a low filtered noise wash under it. Dealt damage should
// read as impact you caused; taken damage as something landing on you.
var thud = Patch{
	{opEnv, [4]float64{0.002, 0.22, 0, 0.12}},
	{opOsc, [4]float64{-7, shpSine, 14, 0.06}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.2, 0, 0, 0}},
	{opOut, [4]float64{0.95, 0.5}},
	{opEnv, [4]float64{0.004, 0.14, 0, 0.08}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{500, 0, 0, 0}},
	{opLpf, [4]float64{500, 0, 0, 0}},
	{opOut, [4]float64{0.75, 0.5}},
}

// shotDef pairs a patch with its gate length. One-shots are not on the
// sequencer grid, so nothing counts gate rows for them; the gate is in
// milliseconds and expires in frame().
type shotDef struct {
	patch  Patch
	gateMS float64
}

var shotLib = map[string]shotDef{
	"footstep": {footstep, 15},
	"hit":      {hit, 25},
	"thud":     {thud, 40},
	// pickup reuses the Karplus-Strong lead: pitched by the caller from the
	// floor's own scale, which is what keeps found-a-rune in key with the score.
	"pluck": {pluck, 60},
}

// shotReq is a staged trigger. delay is in samples, counted down in frame(),
// so a two-note pickup arpeggio is sample-accurate rather than host-timed.
type shotReq struct {
	patch Patch
	note  int
	gain  float64
	pan   float64
	delay int
	gate  int
	seed  uint64
}

const nShots = 8

// sfx is the Synth's one-shot state, mutex-guarded because Trigger arrives on
// the lg thread while frame() drains on the audio callback goroutine.
type sfx struct {
	mu      sync.Mutex
	pending []shotReq
	waiting atomic.Bool // mirror of len(pending) > 0; lets frame() skip the lock
	shots   [nShots]Voice
	pan     [nShots]float64
	pos     int
}

// Trigger stages a one-shot. Unknown names are dropped, not errors: the sfx
// vocabulary lives above this seam, and a stale name must not crash a game.
// gain scales the patch's own levels; pan is 0..1; delayMS offsets the start.
func (s *Synth) Trigger(name string, note int, gain, pan, delayMS float64, seed uint64) bool {
	d, ok := shotLib[name]
	if !ok {
		return false
	}
	if pan < 0 {
		pan = 0
	} else if pan > 1 {
		pan = 1
	}
	req := shotReq{
		patch: humanize(d.patch, seed),
		note:  note,
		gain:  gain,
		pan:   pan,
		delay: int(delayMS * SampleRate / 1000),
		gate:  int(d.gateMS * SampleRate / 1000),
		seed:  seed,
	}
	s.fx.mu.Lock()
	s.fx.pending = append(s.fx.pending, req)
	s.fx.waiting.Store(true)
	s.fx.mu.Unlock()
	return true
}

// humanize nudges a copy of the patch's envelope decays and filter cutoffs,
// deterministically from the trigger seed. This is what keeps a footstep every
// turn from machine-gunning: the patch tables stay fixed, the instance varies.
func humanize(p Patch, seed uint64) Patch {
	r := rng{seed*2862933555777941757 + 3037000493 | 1}
	out := make(Patch, len(p))
	copy(out, p)
	for i := range out {
		switch out[i].op {
		case opEnv:
			out[i].p[1] *= 0.9 + 0.2*r.f()
		case opLpf:
			out[i].p[0] *= 0.85 + 0.3*r.f()
		}
	}
	return out
}

// runShots activates due triggers and mixes the live one-shot voices.
// Called once per frame; the fast path (no shots, nothing pending) is two
// comparisons and no lock.
func (s *Synth) runShots(l, r *float64) {
	fx := &s.fx
	if fx.waiting.Load() {
		fx.mu.Lock()
		keep := fx.pending[:0]
		for _, q := range fx.pending {
			if q.delay > 0 {
				q.delay--
				keep = append(keep, q)
				continue
			}
			i := fx.pos % nShots
			fx.pos++
			fx.shots[i].trigger(q.patch, q.note, 0, q.gain, q.seed)
			fx.shots[i].gateSamples = q.gate
			fx.pan[i] = q.pan
		}
		fx.pending = keep
		fx.waiting.Store(len(keep) > 0)
		fx.mu.Unlock()
	}
	for i := range fx.shots {
		v := &fx.shots[i]
		if !v.active {
			continue
		}
		if v.gateSamples > 0 {
			v.gateSamples--
			if v.gateSamples == 0 {
				v.gate = false
			}
		}
		var sl, sr float64
		v.run(&sl, &sr, &s.dbus)
		mono := sl + sr
		*l += mono * (1 - fx.pan[i])
		*r += mono * fx.pan[i]
	}
}
