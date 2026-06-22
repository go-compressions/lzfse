#!/usr/bin/env bash
# Reproducible end-to-end parity run for go-compressions/lzfse vs Apple's
# reference lzfse C library, on the same machine.
#
#   1. Builds Apple's reference liblzfse + the in-memory C harness.
#   2. Fetches/generates the corpus.
#   3. Runs the Go harness, which times our codec and shells out to the C
#      harness for the reference numbers.
#
# Env:
#   LZFSE_SRC  path to a github.com/lzfse/lzfse checkout (default: clone to /tmp)
#   ITERS      timed iterations per file (default 21)
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
iters="${ITERS:-21}"

# --- 1. reference liblzfse + C harness ----------------------------------------
lzfse_src="${LZFSE_SRC:-/tmp/lzfse-ref}"
if [ ! -d "$lzfse_src/src" ]; then
  git clone --depth 1 https://github.com/lzfse/lzfse.git "$lzfse_src"
fi
# Build with -O3 (Apple ships -Os; -O3 gives the reference its best speed, the
# fairest "authoritative bar").
make -C "$lzfse_src" clean >/dev/null 2>&1 || true
make -C "$lzfse_src" install INSTALL_PREFIX="$lzfse_src/out" \
  CFLAGS="-O3 -Wall -Wno-unknown-pragmas -Wno-unused-variable -DNDEBUG -D_POSIX_C_SOURCE -std=c99 -fvisibility=hidden" >/dev/null
ref_bin="$here/ref/lzfse_bench"
cc -O3 -std=c99 -I"$lzfse_src/out/include" "$here/ref/lzfse_bench.c" \
  "$lzfse_src/out/lib/liblzfse.a" -o "$ref_bin"

# --- 2. corpus ----------------------------------------------------------------
bash "$here/fetch_corpus.sh"

# --- 3. run -------------------------------------------------------------------
cd "$here"
go run . -corpus ./corpus -ref "$ref_bin" -iters "$iters"
