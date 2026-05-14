# lzfse

[![ci](https://github.com/go-compressions/lzfse/actions/workflows/ci.yml/badge.svg)](https://github.com/go-compressions/lzfse/actions/workflows/ci.yml)
![coverage](https://img.shields.io/badge/coverage-93.8%25-brightgreen)

Pure-Go implementation of Apple's **LZFSE** and **LZVN** compression formats.
Byte-compatible with the reference `liblzfse` C implementation: data compressed
by `liblzfse` round-trips through `Decompress`, and data produced by `Compress`
is decoded by `liblzfse` without modification.

## Module

```
github.com/go-compressions/lzfse
```

## API

```go
func Compress(src []byte) ([]byte, error)
func Decompress(src []byte) ([]byte, error)
```

`Compress` picks the format automatically: inputs ≤ 4 KiB are emitted as an
LZVN block (one `bvxn` block followed by `bvx$` end-of-stream); larger inputs
are emitted as LZFSE blocks (V1/V2 headers + FSE-encoded streams).
`Decompress` handles every block magic Apple's reference emits:

| Magic  | Bytes  | Meaning                            |
| ------ | ------ | ---------------------------------- |
| `bvx-` | `2D…`  | Uncompressed payload (passthrough) |
| `bvx1` | `31…`  | LZFSE V1 (uncompressed freq table) |
| `bvx2` | `32…`  | LZFSE V2 (variable-length codes)   |
| `bvxn` | `6E…`  | LZVN block                         |
| `bvx$` | `24…`  | End-of-stream marker               |

## Usage

```go
import "github.com/go-compressions/lzfse"

compressed, err := lzfse.Compress(payload)
if err != nil { /* ... */ }

decoded, err := lzfse.Decompress(compressed)
if err != nil { /* ... */ }
```

## Consumers

- `pkg/go-compressions/lzfsec` — CLI wrapper (`lzfsec compress|decompress`).
- `pkg/go-filesystems/apfs` — APFS `decmpfs` transparent decompression for
  types 7 / 8 / 11 / 12 (LZVN / LZFSE, inline + resource-fork variants).
- `pkg/go-diskimages/tart-oci` — Tart layer decompression.

## Development

The package ships a [Taskfile](https://taskfile.dev) for the common build,
test, and lint targets used by both local development and the GitHub Actions
workflow at [.github/workflows/ci.yml](.github/workflows/ci.yml).

```sh
task lint    # go vet
task build   # go build
task test    # go test -race + coverage report
task ci      # lint + build + test, what CI runs
```

Dependency updates are handled by Renovate ([renovate.json](renovate.json));
patch and minor `gomod` updates auto-merge.

## Test coverage

`task test` reports **93.8 % statement coverage** ([`cover.out`](cover.out)).
The remaining ~6 % is split between:

- Error-forwarding branches in `Decompress` that fire only on corrupted V1 /
  V2 / freq-table bit-streams (panic-safety on adversarial input is a known
  hardening gap — the random-garbage fuzz tests run with `recover()`).
- One `findMatches` / `encodeBlock` branch reachable only when the encoder
  generates overlapping matches for certain mixed-compressibility inputs
  (separate known bug — `Compress` panics with `slice bounds out of range`
  on payloads that alternate compressible runs with incompressible noise).

Both gaps are tracked as future work.
