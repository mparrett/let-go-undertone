# _moving

Bound for other repositories; the go tool skips this directory (leading underscore), so nothing here is built or tested in this module. These are snapshots taken before the restructure and still import the old `undertone/...` paths.

| Path | Goes to |
|---|---|
| `web/`, `webdemo/`, `serve.py`, `lg/demo.lg`, `host/demo.go`, `host/embed_test.go`, `lghost-demo/` | let-go-lab `demos/undertone` (the browser pages, their dev server, and the scripted demo arc with its driver and test) |
| `cueplay/` | let-go-lab `experiments/cueplay` |
| `lg/music.lg`, `host/music_test.go`, `xsofymod/`, `tuner/` | xsofy's audio lane |
| `lg/sfx.lg`, `host/sfx_lg_test.go` | delete (frozen; superseded by xsofy's own `sfx.lg`) |
