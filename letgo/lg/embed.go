// Package lg embeds the binding's lg sources, which package letgo registers
// with let-go from init so (require 'undertone.ambience) resolves in any
// binary that links it, with no source path and in the browser too. The .lg
// files stay the single source of truth; this package is only a carrier.
package lg

import _ "embed"

//go:embed ambience.lg
var ambience string

// Source is one embedded namespace: its dotted name and its text.
type Source struct {
	Name string
	Text string
}

// Sources lists the namespaces in dependency order — load-string does not
// consult the resolver for already-registered namespaces' requires to work,
// it just needs each dependency registered before its dependents load.
var Sources = []Source{
	{"undertone.ambience", ambience},
}
