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

// Close silences the player; oto v3.4+ releases it once Stop drops the last
// reference, so there is no Player.Close to call.
func (o *otoOutput) Close()  { o.player.Pause() }
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
