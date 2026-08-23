// Package lg embeds the prototype's lg sources so hosts without a filesystem
// — GOOS=js above all — can load them through core's load-string instead of
// the resolver's load path. The .lg files stay the single source of truth;
// this package is only a carrier.
package lg

import _ "embed"

//go:embed ambience.lg
var ambience string

//go:embed music.lg
var music string

//go:embed sfx.lg
var sfx string

//go:embed demo.lg
var demo string

// Source is one embedded namespace: its dotted name and its text.
type Source struct {
	Name string
	Text string
}

// Sources lists the namespaces in dependency order — load-string does not
// consult the resolver for already-registered namespaces' requires to work,
// it just needs each dependency registered before its dependents load.
var Sources = []Source{
	{"ambience", ambience},
	{"music", music},
	{"sfx", sfx},
	{"demo", demo},
}
