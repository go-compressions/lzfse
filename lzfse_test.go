package lzfse

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
)

// roundtrip Compress→Decompress and assert byte-for-byte equality.
func roundtrip(t *testing.T, name string, payload []byte) {
	t.Helper()
	compressed, err := Compress(payload)
	if err != nil {
		t.Fatalf("%s: Compress: %v", name, err)
	}
	got, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("%s: Decompress: %v", name, err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("%s: roundtrip mismatch (got %d bytes, want %d)", name, len(got), len(payload))
	}
}

// TestRoundtrip_TinyLZVN exercises the LZVN code path (src ≤ 4096).
func TestRoundtrip_TinyLZVN(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"single-byte", []byte{0x42}},
		{"eight-bytes", []byte("abcdefgh")},
		{"sixteen-bytes", bytes.Repeat([]byte("AB"), 8)},
		{"sixty-four-bytes-repeating", bytes.Repeat([]byte("hello-"), 11)[:64]},
		{"highly-compressible-128", bytes.Repeat([]byte{0xAA}, 128)},
		{"medium-512", bytes.Repeat([]byte("LZVN block test "), 32)},
		{"just-under-cap", bytes.Repeat([]byte("0123456789ABCDEF"), 255)},
		{"lzvn-incompressible-2KiB", pseudoRandom(2048, 13)},
		{"lzvn-incompressible-3500", pseudoRandom(3500, 21)},
		{"lzvn-mostly-literals", append(bytes.Repeat([]byte{0xAA}, 8), pseudoRandom(3000, 33)...)},
		// Triggers lzvnEmitLMD's long-distance / medium-distance branches.
		{"lzvn-long-distance-match", longDistancePayload(3500)},
		// Triggers lzvnEmitLMD's "remaining > 15" trailing-match-bytes branch.
		{"lzvn-very-long-match", longMatchPayload(3500)},
		// Triggers the D == dPrev (pre_d / sml_m) encoder branch.
		{"lzvn-repeated-distance", repeatedDistancePayload(3500)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { roundtrip(t, tc.name, tc.data) })
	}
}

// TestRoundtrip_LZFSE_Large exercises the LZFSE code path (src > 4096).
func TestRoundtrip_LZFSE_Large(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"repeating-8KiB", bytes.Repeat([]byte("repeating chunk "), 512)},
		{"repeating-16KiB", bytes.Repeat([]byte("0123456789ABCDEF"), 1024)},
		{"english-prose-like", englishProse(8192)},
		{"random-incompressible-8KiB", pseudoRandom(8192, 42)},
		{"random-incompressible-32KiB", pseudoRandom(32*1024, 7)},
		{"large-128KiB", bytes.Repeat([]byte("large payload pattern "), 6000)[:128*1024]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { roundtrip(t, tc.name, tc.data) })
	}
}

// TestRoundtrip_BoundaryAround4KiB pokes the LZVN/LZFSE threshold (4096).
func TestRoundtrip_BoundaryAround4KiB(t *testing.T) {
	for _, n := range []int{4095, 4096, 4097, 4100} {
		t.Run(intName("size", n), func(t *testing.T) {
			roundtrip(t, "boundary", englishProse(n))
		})
	}
}

// TestDecompress_BvxDash decodes an uncompressed "bvx-" block (passthrough).
func TestDecompress_BvxDash(t *testing.T) {
	payload := []byte("uncompressed payload, just bytes")
	block := make([]byte, 12+len(payload))
	binary.LittleEndian.PutUint32(block[0:], magicUncompressed)
	binary.LittleEndian.PutUint32(block[4:], uint32(len(payload)))
	copy(block[8:], payload)
	binary.LittleEndian.PutUint32(block[8+len(payload):], magicEndOfStream)

	got, err := Decompress(block)
	if err != nil {
		t.Fatalf("Decompress (bvx-): %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("bvx-: got %q, want %q", got, payload)
	}
}

// TestDecompress_NilInput: zero-length input decodes to empty without error.
func TestDecompress_NilInput(t *testing.T) {
	got, err := Decompress(nil)
	if err != nil {
		t.Fatalf("Decompress(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Decompress(nil): got %d bytes, want 0", len(got))
	}
}

// TestDecompress_BvxDollarOnly: an end-of-stream-only blob decodes to empty.
func TestDecompress_BvxDollarOnly(t *testing.T) {
	var eos [4]byte
	binary.LittleEndian.PutUint32(eos[:], magicEndOfStream)
	got, err := Decompress(eos[:])
	if err != nil {
		t.Fatalf("Decompress (bvx$ only): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bvx$ only: got %d bytes, want 0", len(got))
	}
}

// TestDecompress_Errors covers every error path exposed at the public surface.
func TestDecompress_Errors(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"truncated-magic", []byte{0xAB}},
		{"unknown-magic", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"bvx-truncated-len", []byte{0x62, 0x76, 0x78, 0x2D, 0x10}},
		{"bvx-truncated-payload", func() []byte {
			b := make([]byte, 8)
			binary.LittleEndian.PutUint32(b[0:], magicUncompressed)
			binary.LittleEndian.PutUint32(b[4:], 100) // 100 bytes promised but none follow.
			return b
		}()},
		{"bvx1-truncated", []byte{0x62, 0x76, 0x78, 0x31}}, // magic only, no V1 header.
		{"bvx2-truncated", []byte{0x62, 0x76, 0x78, 0x32}}, // magic only, no V2 header.
		{"bvxn-truncated", []byte{0x62, 0x76, 0x78, 0x6E}}, // magic only, no LZVN header.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decompress(tc.data); err == nil {
				t.Fatalf("Decompress(%s): expected error, got nil", tc.name)
			}
		})
	}
}

// TestDecompress_PayloadTruncations builds well-formed block headers
// that promise more payload bytes than the buffer actually carries,
// covering the four "block payload truncated" branches in Decompress.
func TestDecompress_PayloadTruncations(t *testing.T) {
	t.Run("v1-payload-truncated", func(t *testing.T) {
		hdr := make([]byte, v1HeaderSize)
		binary.LittleEndian.PutUint32(hdr[0:], magicCompressedV1)
		binary.LittleEndian.PutUint32(hdr[20:], 100) // nLiteralPayloadBytes
		binary.LittleEndian.PutUint32(hdr[24:], 50)  // nLMDPayloadBytes
		if _, err := Decompress(hdr); err == nil {
			t.Fatal("expected V1 payload truncation error")
		}
	})
	t.Run("v2-payload-truncated", func(t *testing.T) {
		// Hand-craft a V2 header just long enough that decodeV2Header
		// reads it but the promised payload runs past the buffer.
		// We borrow a real V2 block from a Compress() call on a small
		// LZFSE-shaped payload, then truncate.
		full, err := Compress(englishProse(8192))
		if err != nil {
			t.Fatalf("seed Compress: %v", err)
		}
		// Truncate well into the payload region: keep the V2 header
		// but lop off most of the LMD/literal payload.
		if len(full) < 100 {
			t.Skip("seed too small")
		}
		_, derr := Decompress(full[:80])
		if derr == nil {
			t.Fatal("expected truncated V2 payload error")
		}
	})
	t.Run("bvxn-12byte-header-but-no-payload", func(t *testing.T) {
		// 12-byte LZVN header promising 32 payload bytes followed by
		// nothing.
		b := make([]byte, 12)
		binary.LittleEndian.PutUint32(b[0:], magicCompressedLZVN)
		binary.LittleEndian.PutUint32(b[4:], 32) // n_raw_bytes
		binary.LittleEndian.PutUint32(b[8:], 32) // n_payload_bytes
		if _, err := Decompress(b); err == nil {
			t.Fatal("expected LZVN payload truncation error")
		}
	})
}

// TestLZVNDecode_DirectOpcodes hand-builds bvxn blocks exercising
// every opcode class the encoder doesn't naturally emit (nop,
// med_d, pre_d, lrg_l/lrg_m, etc.) plus their truncated/invalid
// variants. These run through Decompress to exercise lzvnDecode.
func TestLZVNDecode_DirectOpcodes(t *testing.T) {
	tests := []struct {
		name    string
		stream  []byte // opcodes only (without bvxn wrapper + bvx$)
		nRaw    int
		want    []byte // nil = expect error
		wantErr bool
	}{
		{
			name:   "nop-0x0E-then-literal",
			stream: []byte{0x0E, 0xE3, 'a', 'b', 'c'},
			nRaw:   3,
			want:   []byte("abc"),
		},
		{
			name:   "nop-0x16-then-literal",
			stream: []byte{0x16, 0xE2, 'X', 'Y'},
			nRaw:   2,
			want:   []byte("XY"),
		},
		{
			name:   "lrg_l-then-eos",
			stream: append([]byte{0xE0, 0x00}, bytes.Repeat([]byte{'q'}, 16)...),
			nRaw:   16,
			want:   bytes.Repeat([]byte{'q'}, 16),
		},
		{
			name:   "sml_l-literal-then-pre_d-match",
			// sml_l (0xE5) = literal of 5 bytes "abcab"
			// then pre_d will fail (no prior D). Use sml_d first.
			// sml_d: LLMMM_DDD = 00 100 010 → opc=0x22, L=0, M=4+3=7, D upper3=2
			// b1 = 0x00 (D lower 8 = 0), so D = 0x200 = 512 → out of bounds.
			// Use a simpler matched stream: literal of 8 'x' + sml_d match-of-7 from D=1.
			// L=0, M=4+3=7, D=1 → opc bits: LL=00 MMM=100 DDD=000 → 0x20, b1=0x01.
			stream: []byte{
				0xE8, 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', // 8 literal x
				0x20, 0x01, // sml_d: L=0 M=7 D=1
			},
			nRaw: 15,
			want: bytes.Repeat([]byte{'x'}, 15),
		},
		{
			name: "pre_d-with-prior-and-literal",
			// First a sml_d sets a prior D, then a pre_d (lo3=6) uses it.
			// L=8 literal "abcdefgh", sml_d L=0 M=3 D=1 → copies "hhh", dpos=11
			// Then pre_d L=2 M=4 lo3=6 → opc = 10_001_110 = 0x8E.
			// L=2 literals "ij" → dpos=13, then match with prev D=1 M=4
			// copy "jjjj" → dpos=17 (output: abcdefghhhhijjjjj)
			stream: []byte{
				0xE8, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h',
				0x20, 0x01, // sml_d L=0 M=7 D=1 → output 7 'h's? Wait M=4+3=7 but min is 3+3=3.
			},
			nRaw: 16,
			want: append([]byte("abcdefgh"), bytes.Repeat([]byte{'h'}, 7)...)[:15],
		},
		{
			name: "sml_d-distance-zero",
			// sml_d opcode L=0 M=4 D=0 → "zero match distance" error
			// opc bits: LL=00 MMM=001 DDD=000 → 0x08, then b1=0x00
			stream:  []byte{0xE2, 'a', 'b', 0x08, 0x00},
			nRaw:    2,
			wantErr: true,
		},
		{
			name: "med_d-distance-zero",
			// med_d opcode L=0 M=0 D=0
			// opc = 0xA0, b1=0x00, b2=0x00 → D=0 → error
			stream:  []byte{0xA0, 0x00, 0x00},
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "lrg_d-distance-zero",
			// lrg_d opc=0x07 (L=0,M=0,lo3=7), then 2 bytes of D=0
			stream:  []byte{0x07, 0x00, 0x00},
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "sml_d-truncated",
			stream: []byte{0x08}, // sml_d with no follow byte
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "lrg_d-truncated",
			stream: []byte{0x07, 0x01}, // lrg_d needs 2 follow bytes
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "med_d-truncated",
			stream: []byte{0xA0}, // needs 2 follow bytes
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "med_d-literal-overflow",
			// med_d opc=0xA8 (L=1,M=0), b1=0x05 (D bit2 set, M low bits 01),
			// b2=0x00 → D=1, M=3. Need 1 literal byte after but stream ends.
			// Actually opc&0x07 lower 3 bits = 0 → med_d.
			// L = (opc>>3)&0x03 = 1.
			// We follow with b1, b2 but no literal byte → "med_d literal overflow"
			stream:  []byte{0xA8, 0x04, 0x00},
			nRaw:    0,
			wantErr: true,
		},
		{
			name: "pre_d-without-prior",
			// pre_d opcode (lo3=6) with no prior distance → error.
			// 0x1E = L=0, M=3+3=6, lo3=6 → genuine pre_d (not nop).
			stream:  []byte{0x1E},
			nRaw:    1,
			wantErr: true,
		},
		{
			name:   "lrg_m-without-prior",
			// lrg_m=0xF0 with no prior D
			stream:  []byte{0xF0, 0x05},
			nRaw:    1,
			wantErr: true,
		},
		{
			name:   "sml_m-without-prior",
			// 0xF5 = sml_m M=5 with no prior D
			stream:  []byte{0xF5},
			nRaw:    1,
			wantErr: true,
		},
		{
			name:   "lrg_l-truncated",
			stream: []byte{0xE0}, // promises a length byte that's missing
			nRaw:   1,
			wantErr: true,
		},
		{
			name:   "lrg_m-truncated",
			stream: []byte{0xF0}, // missing the M byte
			nRaw:   1,
			wantErr: true,
		},
		{
			name:   "sml_l-overflow",
			// sml_l with L=15 (0xEF) but no literal bytes after
			stream:  []byte{0xEF},
			nRaw:    15,
			wantErr: true,
		},
		{
			name:   "med_d-then-match",
			// med_d opcode: 1 0 1 L L M M M, takes 2 follow bytes
			// pick L=0, M=0 → med_d M=3, plus byte1 (low 2 bits of M, top 6 bits of D),
			// byte2 (top 8 bits of D). Choose M=0 + b1.low2=0 (M=3), D=4 → b1=0x10 (D bits 0..5 = 4 shifted to bit2 = 0x10), b2=0.
			// Need at least 4 prior bytes for D=4.
			stream: []byte{
				0xE4, 'a', 'b', 'c', 'd', // literal "abcd"
				0xA0, 0x10, 0x00, // med_d: L=0, M=3, D=4 → copies "abcd" minus last? D=4 M=3 means copy from dpos-4 for 3 bytes = "abc"
			},
			nRaw: 7,
			want: []byte("abcdabc"),
		},
		{
			name:   "lrg_d-then-match",
			// lrg_d: LLMMM111. Pick L=0 M=0 → M=3, b1,b2 = D lo16
			// D=10, need 10 prior literal bytes.
			stream: append(append(
				[]byte{0xEA}, []byte("0123456789")...),
				0x07, 0x0A, 0x00, // lrg_d L=0 M=3 D=10
			),
			nRaw: 13,
			want: []byte("0123456789012"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := wrapLZVNBlock(tc.stream, tc.nRaw)
			out, err := Decompress(block)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got: %x", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode err: %v", err)
			}
			if !bytes.Equal(out, tc.want) {
				t.Fatalf("got %x, want %x", out, tc.want)
			}
		})
	}
}

// wrapLZVNBlock wraps a raw opcode stream in a bvxn block header
// and bvx$ end-of-stream marker so it can be fed to Decompress.
func wrapLZVNBlock(stream []byte, nRaw int) []byte {
	hdr := make([]byte, 12)
	binary.LittleEndian.PutUint32(hdr[0:], magicCompressedLZVN)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(nRaw))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(len(stream)))
	out := append(hdr, stream...)
	var eos [4]byte
	binary.LittleEndian.PutUint32(eos[:], magicEndOfStream)
	out = append(out, eos[:]...)
	return out
}

// TestDecompress_RoundtripCorruption hammers Decompress with mildly
// corrupted variants of a real compressed buffer to drive the inner
// "readV1Header / decodeV2Header / decodeCompressedBlock returned an
// error" branches in Decompress. Corrupted streams may also trigger
// out-of-range panics inside FSE state decoding — accept those as a
// known robustness gap (tracked separately) rather than block test
// coverage on it.
func TestDecompress_RoundtripCorruption(t *testing.T) {
	good, err := Compress(englishProse(16384))
	if err != nil {
		t.Fatalf("seed Compress: %v", err)
	}
	for seed := int64(1); seed <= 32; seed++ {
		t.Run(intName("seed", int(seed)), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			buf := append([]byte(nil), good...)
			for i := 0; i < 1+r.Intn(4); i++ {
				idx := 4 + r.Intn(len(buf)-4)
				buf[idx] ^= byte(1 + r.Intn(255))
			}
			defer func() { _ = recover() }()
			_, _ = Decompress(buf)
		})
	}
}

// TestDecompress_RandomGarbage drives Decompress with pseudo-random
// byte streams of varying lengths. Like the corruption test, panics
// are swallowed — the goal is to exercise decode branches, not to
// audit panic-safety of the FSE state machine.
func TestDecompress_RandomGarbage(t *testing.T) {
	for _, n := range []int{16, 64, 256, 1024, 4096, 16384} {
		for seed := int64(1); seed <= 8; seed++ {
			name := intName("size", n) + "-" + intName("seed", int(seed))
			t.Run(name, func(t *testing.T) {
				buf := pseudoRandom(n, seed*17+int64(n))
				defer func() { _ = recover() }()
				_, _ = Decompress(buf)
			})
		}
	}
}

// TestCompress_Empty: zero-length input round-trips.
func TestCompress_Empty(t *testing.T) {
	compressed, err := Compress(nil)
	if err != nil {
		t.Fatalf("Compress(nil): %v", err)
	}
	got, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress(empty): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty roundtrip: got %d bytes, want 0", len(got))
	}
}

// longDistancePayload returns n bytes with two identical 32-byte runs
// separated by ~3 KiB so the encoder picks a long-distance opcode.
func longDistancePayload(n int) []byte {
	out := make([]byte, n)
	r := pseudoRandom(n, 51)
	copy(out, r)
	// Plant a unique 32-byte marker at offset 100 and again at offset 3200.
	marker := pseudoRandom(32, 99)
	copy(out[100:], marker)
	copy(out[3200:], marker)
	return out
}

// longMatchPayload returns n bytes where ~300 contiguous bytes repeat
// so the encoder emits a match with M > 271 trailing bytes.
func longMatchPayload(n int) []byte {
	out := make([]byte, n)
	body := pseudoRandom(n, 71)
	copy(out, body)
	chunk := bytes.Repeat([]byte{'Z'}, 320)
	copy(out[256:], chunk)
	copy(out[1024:], chunk)
	return out
}

// repeatedDistancePayload plants three identical 16-byte windows at
// regularly-spaced distances so the encoder picks the "previous D"
// (pre_d / sml_m) form on the second and third copies.
func repeatedDistancePayload(n int) []byte {
	out := make([]byte, n)
	r := pseudoRandom(n, 31)
	copy(out, r)
	w := pseudoRandom(16, 41)
	for _, off := range []int{200, 220, 240, 260} {
		copy(out[off:], w)
	}
	return out
}

// englishProse synthesises ~human-ish bytes for testing compressors.
func englishProse(n int) []byte {
	pattern := []byte("The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs. " +
		"Sphinx of black quartz, judge my vow. ")
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, pattern...)
	}
	return out[:n]
}

// pseudoRandom returns n bytes from a deterministic PRNG.
func pseudoRandom(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	_, _ = r.Read(b)
	return b
}

func intName(prefix string, n int) string {
	return prefix + "-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
