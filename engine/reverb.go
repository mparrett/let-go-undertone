package engine

// sends are the buses a voice feeds besides its own output: the delay and
// the reverb, each mixed back in after the voices.
type sends struct{ delay, verb float64 }

// reverb is a Freeverb-style network, after Jezar's public-domain design:
// per channel, eight damped feedback combs in parallel, then four allpasses in
// series, the right channel's lines a little longer for width. The tunings are
// Freeverb's, for 44.1 kHz.
type reverb struct {
	comb [2][8]combLine
	ap   [2][4]allpass
	on   bool // set by the first send; a synth that never sends skips the work
}

type combLine struct {
	buf   []float64
	pos   int
	store float64 // the damping lowpass
}

type allpass struct {
	buf []float64
	pos int
}

var (
	combTuning = [8]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	apTuning   = [4]int{556, 441, 341, 225}
)

const (
	verbSpread   = 23    // samples added to each right-channel line
	verbIn       = 0.015 // Freeverb's fixed input gain
	verbFeedback = 0.94  // comb feedback: a long, dark room (Freeverb room 0.86)
	verbDamp     = 0.3   // share of each comb's output lowpassed away per pass
	verbWet      = 0.45  // sets the tail against the dry: about 6 dB under a sustained bed
)

func newReverb() *reverb {
	v := &reverb{}
	for ch := range 2 {
		for i, n := range combTuning {
			v.comb[ch][i].buf = make([]float64, n+ch*verbSpread)
		}
		for i, n := range apTuning {
			v.ap[ch][i].buf = make([]float64, n+ch*verbSpread)
		}
	}
	return v
}

// process takes one sample of the reverb bus and returns the wet stereo pair.
func (v *reverb) process(in float64) (float64, float64) {
	if in != 0 {
		v.on = true
	}
	if !v.on {
		return 0, 0
	}
	in *= verbIn
	var out [2]float64
	for ch := range 2 {
		sum := 0.0
		for i := range v.comb[ch] {
			c := &v.comb[ch][i]
			y := c.buf[c.pos]
			c.store = flush(y*(1-verbDamp) + c.store*verbDamp)
			c.buf[c.pos] = in + c.store*verbFeedback
			c.pos++
			if c.pos == len(c.buf) {
				c.pos = 0
			}
			sum += y
		}
		for i := range v.ap[ch] {
			a := &v.ap[ch][i]
			y := a.buf[a.pos]
			a.buf[a.pos] = flush(sum + y*0.5)
			sum = y - sum
			a.pos++
			if a.pos == len(a.buf) {
				a.pos = 0
			}
		}
		out[ch] = sum * verbWet
	}
	return out[0], out[1]
}
