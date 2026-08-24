package engine

import (
	"testing"
	"time"
)

// oto turns our DeviceBuffer duration back into bytes by truncating integer
// arithmetic, then hands bytes/4/channelCount to createScriptProcessor on
// browsers without AudioWorklet — an API that throws unless that number is a
// power of two. So the round trip, not just the intent, has to land on one.
// This reproduces oto's own arithmetic (context.go / driver_js.go).
func TestDeviceBufferSurvivesOtosRoundTripAsAPowerOfTwo(t *testing.T) {
	const channelCount = 2
	bytesPerSecond := SampleRate * bytesPerFrame

	b := int(int64(DeviceBuffer) * int64(bytesPerSecond) / int64(time.Second))
	b = b / bytesPerFrame * bytesPerFrame // oto's alignment step
	frames := b / 4 / channelCount

	if frames&(frames-1) != 0 || frames == 0 {
		t.Errorf("device buffer round-trips to %d frames, which is not a power of two; "+
			"ScriptProcessorNode would throw IndexSizeError and audio would die on "+
			"browsers without AudioWorklet", frames)
	}
	if int64(frames) != deviceFrames {
		t.Errorf("device buffer round-trips to %d frames, want %d", frames, deviceFrames)
	}
}

func TestPlayerBufferIsWellFormedAndSmallerThanOtosDefault(t *testing.T) {
	got := PlayerBufferBytes()
	if got%bytesPerFrame != 0 {
		t.Errorf("player buffer %d bytes is not a whole number of frames", got)
	}
	// oto's default is half a second, which is the latency this exists to cut.
	otoDefault := SampleRate * bytesPerFrame / 2
	if got >= otoDefault {
		t.Errorf("player buffer %d >= oto's default %d — the point is to be smaller", got, otoDefault)
	}
	if PlayerBuffer < DeviceBuffer {
		t.Errorf("player buffer (%v) below device buffer (%v): the device drains in whole "+
			"buffers, so read-ahead shorter than one cannot help", PlayerBuffer, DeviceBuffer)
	}
}
