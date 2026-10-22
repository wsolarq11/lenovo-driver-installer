module lenovo-driver

go 1.27.1

// The toolchain is pinned to an exact patch release because the gate's coverage
// floors are read off `go test -cover`, and Go's statement instrumentation
// changes between patch releases: one patch difference moved internal/trust
// from 45.8% (33/72 statements) to 34.5% (19/55) with no source change, which
// is a floor that passes or fails depending on who runs it. The gate step
// `the running Go toolchain is the one the module pins` fails when a run uses a
// different compiler, so the floors always mean the same thing.
toolchain go1.27.2

require github.com/egoist/mygo v0.6.2

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
