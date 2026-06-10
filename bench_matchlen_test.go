package lzfse

import (
	"bytes"
	"math/rand"
	"testing"
)

// makeBenchInput builds a ~2 MB buffer that mixes redundancy (repeated and
// overlapping content, which exercises the LZ match-extension path) with
// random bytes (which break matches and keep the data realistic).
func makeBenchInput() []byte {
	const size = 2 << 20 // 2 MiB
	rng := rand.New(rand.NewSource(1))
	out := make([]byte, 0, size)

	// A pool of "phrases" that recur, producing long extendable matches.
	phrases := make([][]byte, 16)
	for i := range phrases {
		p := make([]byte, 32+rng.Intn(480))
		rng.Read(p)
		phrases[i] = p
	}

	for len(out) < size {
		switch rng.Intn(4) {
		case 0:
			// Random run (incompressible).
			r := make([]byte, 16+rng.Intn(64))
			rng.Read(r)
			out = append(out, r...)
		default:
			// Repeated phrase, sometimes appended several times in a row to
			// create long overlapping runs.
			p := phrases[rng.Intn(len(phrases))]
			reps := 1 + rng.Intn(8)
			for r := 0; r < reps; r++ {
				out = append(out, p...)
			}
		}
	}
	return out[:size]
}

var benchInput = makeBenchInput()

func BenchmarkEncode(b *testing.B) {
	src := benchInput
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(src); err != nil {
			b.Fatal(err)
		}
	}
}

// TestBenchRoundTrip is a sanity check that the benchmark input round-trips and
// reports the compressed size (used to compare ratio across versions).
func TestBenchRoundTrip(t *testing.T) {
	src := benchInput
	comp, err := Compress(src)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decompress(comp)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, src) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(dec), len(src))
	}
	t.Logf("input=%d compressed=%d ratio=%.4f", len(src), len(comp), float64(len(comp))/float64(len(src)))
}
