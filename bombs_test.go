package lzfse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
)

// allocated runs f and returns the bytes it allocated.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

// lzvnBlock is an LZVN block declaring raw bytes over the given payload.
func lzvnBlock(raw uint32, payload []byte) []byte {
	b := le32(le32([]byte("bvxn"), raw), uint32(len(payload)))
	return append(append(b, payload...), "bvx$"...)
}

// v1Block is a V1 block header (772 bytes) with the given counts and no payload.
func v1Block(raw, literals, matches uint32) []byte {
	b := make([]byte, v1HeaderSize)
	copy(b, "bvx1")
	binary.LittleEndian.PutUint32(b[4:], raw)
	binary.LittleEndian.PutUint32(b[12:], literals)
	binary.LittleEndian.PutUint32(b[16:], matches)
	return append(b, "bvx$"...)
}

// Measured before the fix: this 17-byte stream allocated 1 GiB (the field
// allows 4 GiB) and returned no error.
func TestAnLZVNBlockCannotDeclareMoreThanItsPayloadEncodes(t *testing.T) {
	data := lzvnBlock(1<<30, []byte{0x06})
	var err error
	grew := allocated(func() { _, err = Decompress(data) })
	if err == nil || errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want the structural refusal", err)
	}
	if grew > 1<<20 {
		t.Fatalf("allocated %d bytes for a %d-byte stream", grew, len(data))
	}
	// 136 bytes per payload byte is the bound: one more is refused.
	if _, err := Decompress(lzvnBlock(137, []byte{0x06})); err == nil {
		t.Fatal("137 raw bytes over one payload byte must be refused")
	}
}

// A V1 header's counts are checked before decodeCompressedBlock allocates
// them, as the V2 path always was.
func TestAV1BlockIsCheckedBeforeAllocating(t *testing.T) {
	cases := map[string][]byte{
		"literals past the reference limit": v1Block(4, literalsPerBlock+4, 0),
		"matches past the reference limit":  v1Block(4, 4, uint32(matchesPerBlock)+1),
		"raw past literals and matches":     v1Block(1<<30, 4, 0),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			grew := allocated(func() { _, err = Decompress(data) })
			if err == nil {
				t.Fatal("expected an error")
			}
			if grew > 1<<20 {
				t.Fatalf("allocated %d bytes", grew)
			}
		})
	}
}

func TestDecompressLimitIsExactForEveryBlockKind(t *testing.T) {
	src := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 400)
	c, err := Compress(src)
	if err != nil {
		t.Fatal(err)
	}
	streams := map[string]struct {
		data []byte
		size int
	}{
		"compressed":   {c, len(src)},
		"uncompressed": {append(append(le32([]byte("bvx-"), 10), "0123456789"...), "bvx$"...), 10},
	}
	for name, s := range streams {
		t.Run(name, func(t *testing.T) {
			out, err := DecompressLimit(s.data, s.size)
			if err != nil || len(out) != s.size {
				t.Fatalf("at the ceiling: %d bytes, %v", len(out), err)
			}
			if _, err := DecompressLimit(s.data, s.size-1); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("one under: err = %v, want ErrTooLarge", err)
			}
		})
	}
	// The ceiling is checked from the header, before the block is decoded.
	if _, err := DecompressLimit(v1Block(4, 4, 0), 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("V1: err = %v, want ErrTooLarge", err)
	}
	if _, err := DecompressLimit(lzvnBlock(8, []byte{0x06}), 7); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("LZVN: err = %v, want ErrTooLarge", err)
	}
}

// The encoder's V2 block, its declared raw size made impossible.
func TestAV2BlockCannotDeclareMoreThanItsSymbolsProduce(t *testing.T) {
	src := bytes.Repeat([]byte("abcdefgh"), 2000)
	c, err := Compress(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(c[:4]) != "bvx2" {
		t.Fatalf("the encoder wrote %q, not a V2 block; this test needs one", c[:4])
	}
	binary.LittleEndian.PutUint32(c[4:], 1<<30)
	var derr error
	grew := allocated(func() { _, derr = Decompress(c) })
	if derr == nil || errors.Is(derr, ErrTooLarge) {
		t.Fatalf("err = %v, want the structural refusal", derr)
	}
	if grew > 1<<20 {
		t.Fatalf("allocated %d bytes", grew)
	}
}

func FuzzDecompress(f *testing.F) {
	c, _ := Compress(bytes.Repeat([]byte("fuzz seed "), 100))
	f.Add(c)
	f.Add(lzvnBlock(8, []byte{0x06, 0, 0, 0, 0, 0, 0, 0}))
	f.Add(v1Block(4, 4, 0))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := DecompressLimit(data, 1<<20)
		if err == nil && len(out) > 1<<20 {
			t.Fatalf("returned %d bytes past a 1 MiB ceiling", len(out))
		}
	})
}
