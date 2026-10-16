//go:build js

package letgo

import (
	"io"

	"github.com/mparrett/undertone/engine"
	"github.com/mparrett/undertone/webaudio"
)

// In the browser the engine plays through webaudio's pump rather than oto,
// whose js backend crackles on phones (see package webaudio).
func openOutput(src io.Reader) (output, error) {
	return webaudio.Open(src, webaudio.Options{SampleRate: engine.SampleRate})
}
