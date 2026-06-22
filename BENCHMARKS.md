# Performance parity — go-compressions/lzfse vs Apple reference lzfse  (2026-06-22)

**Methodology**

- **Host:** Apple M4 Max, macOS 26.5, single core (no parallelism in either codec).
- **Our codec:** `github.com/go-compressions/lzfse` built with Go 1.26.4, `CGO_ENABLED=0`.
- **Reference:** Apple's `github.com/lzfse/lzfse` C library (commit `e634ca5`), built
  `-O3` (Apple ships `-Os`; `-O3` gives the reference its best speed — the fairest
  "authoritative bar"). Timed **in-memory** via `benchmarks/ref/lzfse_bench.c` linked
  against `liblzfse`, so both sides time pure in-process encode/decode of a resident
  buffer — no file or process I/O on either side.
- **Corpus:** the Silesia corpus (text / binary / database / structured) plus three
  synthetic 8 MiB edge cases (zeros, random, highly repetitive).
- **Iterations:** 21 timed iterations per file after 3 warm-up rounds; the table reports
  **best** (lowest-noise) throughput. Ratio = compressed ÷ original (lower is better).
- **Correctness:** every file is round-trip verified (`Decompress(Compress(x)) == x`)
  before timing — the `rt` column. (Byte-level cross-compatibility with Apple's stream
  is a separate matter; see "Cross-compatibility" below.)

Decode throughput below is **after** the decode-path rework landed (2026-06-22):
amortised stream growth, an overlap-aware bulk match-copy, and a word-at-a-time
FSE bit-refill. The per-file before→after table follows in the next section.

| file | our comp MB/s | ref comp MB/s | our decomp MB/s | ref decomp MB/s | our ratio | ref ratio | rt |
|------|--------------:|--------------:|----------------:|----------------:|----------:|----------:|:--:|
| dickens             |  31.6 |  120.8 |   409.4 |  1237.7 | 0.391 | 0.379 | ok |
| mozilla             |  34.4 |  180.2 |   586.5 |  1261.8 | 0.370 | 0.367 | ok |
| mr                  |  40.6 |  159.0 |   505.1 |  1131.5 | 0.363 | 0.359 | ok |
| nci                 | 100.3 |  279.9 |  1607.3 |  3933.1 | 0.099 | 0.096 | ok |
| ooffice             |  41.4 |  139.9 |   451.2 |  1162.8 | 0.508 | 0.505 | ok |
| osdb                |  58.4 |  200.4 |   835.8 |  1843.1 | 0.353 | 0.347 | ok |
| reymont             |  38.3 |  155.3 |   473.0 |  1269.6 | 0.309 | 0.303 | ok |
| samba               |  54.8 |  208.4 |   796.3 |  1733.9 | 0.240 | 0.241 | ok |
| sao                 |  44.8 |  164.3 |   496.0 |  1050.1 | 0.767 | 0.750 | ok |
| webster             |  29.1 |  136.4 |   517.8 |  1252.9 | 0.301 | 0.294 | ok |
| x-ray               |  44.8 |  144.4 |   418.8 |   934.7 | 0.717 | 0.707 | ok |
| xml                 |  87.4 |  254.0 |  1138.4 |  2699.6 | 0.129 | 0.128 | ok |
| synth_random.bin    |  90.0 |  279.8 | 56712.0 | 74898.3 | 1.000 | 1.000 | ok |
| synth_repetitive.bin| 565.5 |  240.1 | 11754.9 | 13168.9 | 0.001 | 0.001 | ok |
| synth_zeros.bin     | 354.4 |  240.4 | 11975.9 |  3945.7 | 0.001 | 0.001 | ok |

## Decode throughput — before → after the decode-path rework

Three changes, all decode-only and ratio-neutral, on the same host / corpus /
iteration count:

1. **Amortised stream growth** — the per-block output buffer grew to the *exact*
   size each block, reallocating and copying the whole decoded-so-far stream
   every block (O(n²) over a multi-block file). It now doubles, making the total
   copy work linear. This was the dominant cost on the large real files and is
   the bulk of the win below.
2. **Overlap-aware bulk match-copy** — the byte-at-a-time `out[matchStart+k]`
   loop (and the LZVN decoder's equivalent) became a single `copy()` for
   non-overlapping runs and an exponential pattern-fill for the overlapping
   run-length case. This is the headline on the synthetic runs.
3. **Word-at-a-time FSE bit-refill** — `fseInFlush` now loads a full 8-byte
   little-endian word and masks, instead of a per-byte shift loop, on the common
   in-range path.

| file | decode MB/s before | decode MB/s after | speed-up | ref decode MB/s | gap before | gap after |
|------|-------------------:|------------------:|---------:|----------------:|-----------:|----------:|
| dickens             |   158.1 |   409.4 | 2.59× | 1237.7 |  7.8× | 3.0× |
| mozilla             |   122.5 |   586.5 | 4.79× | 1261.8 | 11.0× | 2.2× |
| mr                  |   223.1 |   505.1 | 2.26× | 1131.5 |  5.6× | 2.2× |
| nci                 |   384.1 |  1607.3 | 4.18× | 3933.1 | 10.2× | 2.4× |
| ooffice             |   224.2 |   451.2 | 2.01× | 1162.8 |  5.1× | 2.6× |
| osdb                |   324.8 |   835.8 | 2.57× | 1843.1 |  5.5× | 2.2× |
| reymont             |   234.6 |   473.0 | 2.02× | 1269.6 |  5.7× | 2.7× |
| samba               |   235.5 |   796.3 | 3.38× | 1733.9 |  7.5× | 2.2× |
| sao                 |   259.2 |   496.0 | 1.91× | 1050.1 |  4.0× | 2.1× |
| synth_repetitive.bin|  1478.7 | 11754.9 | 7.95× |13168.9 |  9.0× | 1.1× |
| synth_zeros.bin     |  1454.9 | 11975.9 | 8.23× | 3945.7 |  2.7× | **3.0× faster** |

Real-file decode is now **2.0–4.8× faster** and the gap to Apple's `-O3` C
reference shrank from **~5–11×** to **~2.1–3.0×**. On the overlapping run-length
synthetic, `synth_zeros` now *beats* the reference (11976 vs 3946 MB/s) and
`synth_repetitive` is at parity (11755 vs 13169). Output is byte-identical —
every file, including the cross-block / >½ MiB / incompressible regression cases
from the three earlier bug fixes, round-trips.

## Summary

**Ratio — at parity.** On real data our compressed size tracks Apple's to within
about 1–3 % (e.g. dickens 0.391 vs 0.379, samba 0.240 vs 0.241, nci 0.099 vs 0.096).
Apple is fractionally tighter on most files; we are level on synthetic data. The
entropy stage (FSE) and the L/M/D model are faithful — the small gap is in match
selection, not coding.

**Speed — compression still lags, decompression now close.** On compression we run at
roughly **¼–⅓** of the reference (dickens 32 vs 121 MB/s) — that is the next target.
On **decompression** the decode-path rework (amortised growth + bulk match-copy +
word-at-a-time FSE refill) closed most of the gap: real files now decode at **~⅓–½**
the reference (dickens 409 vs 1238 MB/s) rather than the former ~⅛, a **2–4.8×**
speed-up. We *beat* the reference on the zeros synthetic and are at parity on the
repetitive one; that case is no longer the only place we win.

**Correctness — solid (and three real bugs were fixed building this).** Every file in
the corpus, including 8 MiB synthetic inputs, round-trips byte-for-byte. Establishing
that surfaced and fixed three encoder/decoder defects the prior ≤128 KiB test suite
never reached:

1. **Cross-block back-references** — the decoder rebuilt each block in a fresh buffer,
   so a match referencing output from an earlier block failed with
   *"invalid match distance"* on any input above ~½ MiB. The decoder now resolves match
   distances against the whole decompressed stream.
2. **20-bit `nLiterals` overflow** — a single V2 block carrying > 2²⁰ literals silently
   corrupted its header (this triggered around 2.5 MiB of mixed data). The block splitter
   now caps each block at 512 KiB of raw bytes, keeping the 20-bit fields in range.
3. **Incompressible fallback** — random / already-compressed data was emitted as a
   broken compressed block that failed to decode. It is now stored in an uncompressed
   block, matching reference LZFSE.

## Root cause of the speed gap

- **Compression:** our match-finder is a straightforward single-cell hash chain in
  portable Go; Apple's encoder is a tuned C hash-table with a wider, history-aware
  search and far less per-byte overhead. The FSE encode also does work per symbol that
  C does with table lookups the compiler vectorises.
- **Decompression (largely addressed 2026-06-22):** the former gap was three things —
  an O(n²) per-block stream re-grow (exact-size realloc+copy every block), a
  byte-at-a-time match copy, and a per-byte FSE bit-refill. Profiling a multi-block file
  showed the stream re-grow, not the entropy decode, dominated. All three are fixed (see
  the before→after table). The residual gap to Apple is the remaining per-symbol FSE
  decode overhead, which Apple's C compiles to branch-light table lookups.

## Action items (to close the gap, in priority order)

1. ~~**Decompression match copy**~~ — **DONE (2026-06-22).** Overlap-aware bulk copy
   landed in both the LZFSE and LZVN decoders (single `copy()` for non-overlapping runs,
   exponential pattern-fill for overlap). Together with the amortised stream growth and
   word-at-a-time FSE refill, decode is 2–4.8× faster on real files. A `go-asmgen` SIMD
   copy kernel on the 6 targets remains optional: the bulk `copy()` already hits the
   runtime's SIMD memmove, so it would only help the short-pattern overlap tail — profile
   to confirm before adding the asm surface.
2. **FSE reader/writer** — the remaining per-symbol decode cost. Hoist the bit
   accumulator into registers, decode L/M/D with fewer branches, and batch the 4
   interleaved literal streams. (The bit-refill is already word-at-a-time.)
3. **Match-finder** — widen the hash table and add lazy matching (the lever
   `go-compressions/lz4` already uses to *beat* its reference on ratio) to both close
   the remaining ratio gap and reduce confirm overhead.
4. **Byte-compatibility with Apple's stream** — see below; a prerequisite for using our
   encoder as a drop-in for `.lzfse` artifacts consumed by Apple tooling.
5. **Parallel blocks** — blocks are independent; a worker pool would give near-linear
   multi-core compression. Tracked separately from the single-core parity bar.

## Cross-compatibility (known limitation)

Our V2 FSE bitstream layout is **not yet byte-identical** to Apple's: a stream we
produce does not decode with Apple's `lzfse` CLI, and vice versa, despite both using the
`bvx2` block magic. Round-trip within our own codec is correct. Achieving bit-exact
interop with the reference bitstream is action item 4 above and is required before we can
claim "byte-compatible with Apple's lzfse".

## Reproduce

```sh
cd benchmarks
./run.sh                       # builds reference -O3, fetches corpus, runs
# or, against an existing corpus + prebuilt C harness:
go run . -corpus ./corpus -ref ./ref/lzfse_bench -iters 21
```

`benchmarks/` is a separate Go module, so it is excluded from the library's
`go test ./...` run and 100 % coverage gate.
