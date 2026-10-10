//go:build js

package main

import (
	"os"
	"syscall/js"
)

// TinyGo's wasm_exec.js does not pass go.argv through, so a TinyGo build
// would see no flags and print usage. The page also sets
// window.undertoneArgs to the same list; read it when os.Args came up empty.
// Stock Go builds already have their arguments and skip this.
func init() {
	if len(os.Args) > 1 {
		return
	}
	a := js.Global().Get("undertoneArgs")
	if a.Type() != js.TypeObject {
		return
	}
	args := []string{"undertone"}
	for i := 1; i < a.Length(); i++ {
		args = append(args, a.Index(i).String())
	}
	os.Args = args
}
