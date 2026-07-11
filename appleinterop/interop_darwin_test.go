//go:build darwin && cgo

// Apple libcompression interop guard.
//
// Checks that the parent lzfse package is byte-exact interoperable with Apple's
// system libcompression (-lcompression, always present on macOS) in BOTH
// directions, across the LZVN (bvxn, <= 4 KiB) and LZFSE (bvx2, > 4 KiB) paths
// and sizes that span single- and multi-block streams:
//
//   - PRODUCE: lzfse.Compress(x) decoded by Apple == x
//   - CONSUME: Apple-compressed(x) decoded by lzfse.Decompress == x
//
// It needs no third-party checkout (unlike the reference liblzfse driver used
// during development), so it runs unattended on a GitHub `macos-latest` runner.
// The whole package is excluded from every non-darwin build by the build tag, so
// it never affects the CGO=0 cross-arch coverage jobs.
package appleinterop

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-compressions/lzfse"
)

// interopSample builds data with mixed compressibility so both the LZVN and
// LZFSE/FSE paths emit real matches and literals.
func interopSample(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	phrase := []byte("the quick brown fox jumps over the lazy dog. ")
	for i := 0; i < n; i++ {
		if r.Intn(3) == 0 {
			b[i] = byte(r.Intn(256))
		} else {
			b[i] = phrase[i%len(phrase)]
		}
	}
	return b
}

func TestAppleInterop(t *testing.T) {
	sizes := []int{
		0, 1, 16, 100, 1000, 4000, // bvx- / bvxn (LZVN) path
		4097, 5000, 16384, 65536, // single bvx2 block
		131072, 262144, 700000, // multi-block bvx2
	}
	for _, n := range sizes {
		src := interopSample(n, int64(n)+12345)

		// PRODUCE: our Compress -> Apple decode must be byte-exact.
		ours, err := lzfse.Compress(src)
		if err != nil {
			t.Fatalf("n=%d Compress: %v", n, err)
		}
		dec, ok := appleLZFSEDecode(ours, n)
		if !ok || !bytes.Equal(dec, src) {
			t.Errorf("n=%d PRODUCE: Apple could not byte-exactly decode our output (ok=%v len=%d/%d)",
				n, ok, len(dec), n)
		}

		// CONSUME: Apple Compress -> our Decompress must be byte-exact.
		appleEnc := appleLZFSEEncode(src)
		got, derr := lzfse.Decompress(appleEnc)
		if derr != nil || !bytes.Equal(got, src) {
			t.Errorf("n=%d CONSUME: we could not byte-exactly decode Apple's output (err=%v len=%d/%d)",
				n, derr, len(got), n)
		}
	}
}

// TestAppleInteropLZVN guards the raw-LZVN block format (CompressLZVN /
// DecompressLZVN) against Apple's COMPRESSION_LZVN in both directions. This
// is the format APFS decmpfs stores for type-7 (inline) and type-8
// (resource-fork) transparently-compressed files, so byte-exact interop
// here is what lets go-filesystems/apfs write compressed files apfs.kext
// reads.
func TestAppleInteropLZVN(t *testing.T) {
	// PRODUCE: our CompressLZVN -> Apple LZVN decode must be byte-exact at
	// every size (this is what go-filesystems/apfs relies on when writing
	// type-7/8 decmpfs chunks that apfs.kext then reads).
	for _, n := range []int{0, 1, 7, 8, 16, 100, 1000, 4096, 16384, 65536, 131072} {
		src := interopSample(n, int64(n)+999)
		ours := lzfse.CompressLZVN(src)
		dec, ok := appleLZVNDecode(ours, n)
		if !ok || !bytes.Equal(dec, src) {
			t.Errorf("n=%d PRODUCE: Apple could not byte-exactly decode our LZVN output (ok=%v len=%d/%d)",
				n, ok, len(dec), n)
		}
	}
	// CONSUME: Apple LZVN Compress -> our DecompressLZVN must be byte-exact.
	// LZVN's block format only carries a match/literal engine for inputs of
	// at least lzvnEncodeMinSrcSize (8) bytes; below that Apple emits a
	// degenerate frame the decoder is not required to accept, and real
	// decmpfs chunks are always far larger, so the CONSUME check starts at 8.
	for _, n := range []int{8, 16, 100, 1000, 4096, 16384, 65536, 131072} {
		src := interopSample(n, int64(n)+999)
		appleEnc := appleLZVNEncode(src)
		got, derr := lzfse.DecompressLZVN(appleEnc, n)
		if derr != nil || !bytes.Equal(got, src) {
			t.Errorf("n=%d CONSUME: we could not byte-exactly decode Apple's LZVN output (err=%v len=%d/%d)",
				n, derr, len(got), n)
		}
	}
}
