// Module for the lzfse performance-parity harness. It is intentionally a
// SEPARATE module from github.com/go-compressions/lzfse so that `go test ./...`
// and the 100% coverage gate at the repo root never descend into it — the
// harness is a measurement tool, not part of the library's tested surface.
module github.com/go-compressions/lzfse/benchmarks

go 1.26

require github.com/go-compressions/lzfse v0.0.0

replace github.com/go-compressions/lzfse => ../
