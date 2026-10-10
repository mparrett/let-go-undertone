// lghost embeds let-go with the undertone binding linked in: lg is the control
// language, Go is the DSP engine.
//
// Only control-rate data crosses the boundary — one parameter map per pattern
// (every few seconds) plus one-shot triggers, never per-sample PCM. That is
// the whole point: the per-sample unit VM runs millions of ops/second, so
// putting it in lg would walk straight into the boxed variadic call ABI. The
// layer that genuinely wants to be lg is the one above it: deriving music
// parameters and sfx triggers from game state, which happens at human rates.
//
// The audio namespace comes from the letgo package's init (see its doc for the
// natives). This file is only the native CLI: flags, the filesystem load path,
// and stdout. An lg runtime built by lgx with the letgo package as a Go dep
// does the same job.
//
//	go run ./cmd/lghost -load-path letgo/lg -e '(do (ambience/score! 7 4 0.6) (audio/music-start) (sleep 60000))'
//	go run ./cmd/lghost -load-path letgo/lg -e '(do (ambience/score! 7 4 0.6) (audio/render-wav "out.wav" 20.0))'
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/vm"

	"github.com/mparrett/let-go-undertone/letgo"
)

func main() {
	loadPath := flag.String("load-path", "letgo/lg", "directory holding the lg namespaces")
	nsName := flag.String("ns", "ambience", "lg namespace to require")
	expr := flag.String("e", "", "lg expression to run after the namespace loads")
	flag.Parse()

	_ = letgo.CurrentMusic() // the import registers the audio namespace

	c, err := api.NewLetGo("undertone.host")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// api.Run compiles a single form, so the whole-file lane is require, not a
	// read-and-eval loop.
	c.SetLoadPath([]string{*loadPath})
	if _, err := c.Run("(require '" + *nsName + ")"); err != nil {
		fmt.Fprintln(os.Stderr, "lg:", err)
		os.Exit(1)
	}
	if *expr != "" {
		v, err := c.Run(*expr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lg:", err)
			os.Exit(1)
		}
		if v != vm.NIL {
			fmt.Println(v)
		}
	}
}
