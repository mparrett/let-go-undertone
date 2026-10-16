//go:build !js

package main

import (
	"io"
	"runtime"

	"github.com/ebitengine/oto/v3"

	"github.com/mparrett/undertone/engine"
)

// startPlayback streams s through oto at oto's default buffer sizes: a CLI bed has no
// one-shots waiting behind the read-ahead, so it keeps the deep buffers.
func startPlayback(s io.Reader) (stop func(), err error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   engine.SampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	pl := ctx.NewPlayer(s)
	pl.Play()
	// The caller defers stop for as long as playback should last, which keeps
	// pl reachable; otherwise oto's runtime.AddCleanup may close the player
	// mid-playback — silence, no error (oto#293).
	return func() { runtime.KeepAlive(pl) }, nil
}
