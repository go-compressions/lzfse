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
