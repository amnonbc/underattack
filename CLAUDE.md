# Project Guidelines

## Go Version

Always write for the latest Go version specified in `go.mod`. Do not write backwards-compatible code or use workarounds for older versions.

**Why**: Go's toolchain automatically downloads and uses the correct version during compilation. Users don't need the latest Go installed globally—they get it automatically. This means:
- Use all latest features and idioms
- Benefit from latest compiler optimizations
- No version checks, shims, or defensive coding needed
- Everyone building the project gets the same (latest) behavior

Example: If `go.mod` specifies `go 1.26`, use `slices.Collect()`, `range over int`, and other 1.26+ features without hesitation.
