//go:build js

package main

import (
	"io"

	"github.com/mparrett/let-go-undertone/engine"
	"github.com/mparrett/let-go-undertone/webaudio"
)

// startPlayback opens the browser output and returns at once: webaudio.Open waits for
// the page's unlocking gesture, and the meter has to run meanwhile.
func startPlayback(s io.Reader) (stop func(), err error) {
	go func() {
		p, err := webaudio.Open(s, webaudio.Options{SampleRate: engine.SampleRate})
		if err != nil {
			println("undertone:", err.Error())
			return
		}
		keep = p
	}()
	return func() {}, nil
}

var keep *webaudio.Player
