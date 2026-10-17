//go:build !js

package letgo

import (
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"

	"github.com/mparrett/undertone/engine"
)

// oto allows one context per process, so it is opened once and shared: a
// Music that is stopped and started again reuses it rather than failing.
var (
	otoOnce  sync.Once
	otoCtx   *oto.Context
	otoReady chan struct{}
	otoErr   error
)

// otoOutput keeps the player in a field: oto closes a player once its handle
// is unreachable, even mid-playback (oto#293).
type otoOutput struct{ player *oto.Player }

// Close stops the player and waits out any read in flight, so the synth is
// not read after Stop returns. Player.Close is a deprecated no-op since oto
// v3.4; PauseAndStopReading is what takes the player out of oto's mixer.
func (o *otoOutput) Close()  { o.player.PauseAndStopReading() }
func (o *otoOutput) Pause()  { o.player.Pause() }
func (o *otoOutput) Resume() { o.player.Play() }

func openOutput(src io.Reader) (output, error) {
	otoOnce.Do(func() {
		otoCtx, otoReady, otoErr = oto.NewContext(&oto.NewContextOptions{
			SampleRate:   engine.SampleRate,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
			BufferSize:   engine.DeviceBuffer,
		})
	})
	if otoErr != nil {
		return nil, otoErr
	}
	<-otoReady
	o := &otoOutput{}
	o.player = otoCtx.NewPlayer(src)
	// Before Play: oto's default read-ahead is half a second, which a
	// one-shot has to wait out (see engine.PlayerBuffer).
	o.player.SetBufferSize(engine.PlayerBufferBytes())
	o.player.Play()
	return o, nil
}
