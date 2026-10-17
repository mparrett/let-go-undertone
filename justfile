staticcheck := "honnef.co/go/tools/cmd/staticcheck@2026.2.2"

test:
    go test ./...

# vet and staticcheck, native and for the wasm packages. staticcheck runs from a
# native binary: `go run` under GOOS=js would build the checker for js too.
lint:
    go vet ./...
    GOOS=js GOARCH=wasm go vet ./examples/designer ./webaudio
    go run {{staticcheck}} ./...
    GOBIN="$(mktemp -d)" sh -c 'go install {{staticcheck}} && GOOS=js GOARCH=wasm "$GOBIN/staticcheck" ./examples/designer ./webaudio'

# build the mood designer and serve it on localhost:{{port}}
designer port="8373": designer-build
    python3 -m http.server -d examples/designer {{port}}

designer-build:
    GOOS=js GOARCH=wasm go build -o examples/designer/main.wasm ./examples/designer
    cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/designer/
