package engine

import "time"

// Audio latency budget.
//
// oto's defaults are tuned for streaming music, where a deep buffer costs
// nothing: the device buffer is 2048 frames and the PLAYER reads a further
// half second ahead (mux.defaultBufferSize). For a bed that is correct and
// invisible. For one-shots it is not — a trigger staged now waits for all
// that already-queued audio to drain, so a footstep lands somewhere in the
// next half second. Measured on the tuning bench before this: median 821 ms
// from tap to first sample, ranging 33 ms to over a second depending on where
// in the buffer cycle the tap fell. The variance is the worse half; a late
// sound is survivable, a sound whose delay changes every time cannot be
// played around.
//
// These are the values every host opens its device with. They are a
// trade-off, not a maximum: smaller buffers leave the renderer less slack
// before an underrun, which is audible as a click — and in the game the same
// main thread also draws the terminal. If dropouts appear, raise DeviceBuffer
// first (in the power-of-two steps below), since it bounds how long the
// renderer may stall.
const (
	// deviceFrames is the device buffer in frames per channel, and it MUST be
	// a power of two. On browsers without AudioWorklet, oto falls back to
	// ScriptProcessorNode and passes exactly this number to
	// createScriptProcessor (driver_js.go), whose spec accepts only
	// 256/512/…/16384 and throws IndexSizeError otherwise — which would kill
	// audio outright on those browsers while working fine everywhere we test.
	// oto's own default is 2048; 512 is two steps down.
	deviceFrames int64 = 512

	// DeviceBuffer is deviceFrames expressed as the duration oto wants,
	// rounded UP. oto converts back by truncating, so a duration that divides
	// exactly-but-short lands a byte low and then aligns down to one frame
	// less — turning 512 into 511 and losing the power of two. Rounding up
	// costs a fraction of a millisecond and makes the round trip exact.
	// (A flat slack term does not work: 1 ms is 22 frames too many.)
	deviceBytes  = deviceFrames * 8
	DeviceBuffer = time.Duration((deviceBytes*int64(time.Second) + SampleRate*bytesPerFrame - 1) /
		(SampleRate * bytesPerFrame))

	// PlayerBuffer is the player's read-ahead, and the knob that actually
	// moves latency. Measured on the bench, tap to first sample tracks it
	// almost linearly — about a 90 ms fixed floor plus 1.4x this value:
	//
	//	 20 ms -> 117 ms      60 ms -> 187 ms
	//	 40 ms -> 129 ms     120 ms -> 256 ms
	//	500 ms -> 821 ms  (oto's default, before any of this)
	//
	// 40 ms is chosen for slack rather than for the last few milliseconds:
	// with DeviceBuffer it leaves ~63 ms for the renderer to stall before an
	// underrun, on a main thread that in the game is also drawing the
	// terminal. Verified dropout-free under continuous play at this value.
	// Lower it for snappier one-shots, but re-check for clicks — and note
	// that a rAF-sampled probe cannot see a short underrun, so "no dropouts
	// measured" is weaker evidence than it sounds.
	PlayerBuffer = 40 * time.Millisecond
)

// PlayerBufferBytes is PlayerBuffer as oto's SetBufferSize wants it: bytes,
// aligned to a whole frame (2 channels x 2 bytes for signed 16-bit LE).
func PlayerBufferBytes() int {
	n := int(int64(PlayerBuffer) * int64(SampleRate) * bytesPerFrame / int64(time.Second))
	return n / bytesPerFrame * bytesPerFrame
}

const bytesPerFrame = 2 * 2 // stereo, signed 16-bit
