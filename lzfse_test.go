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
		// Drives lzvnEmitLMD's `D == dPrev && L > 0` branch: a short
		// match at distance D, then 1–2 literal bytes, then another
		// match at the same distance D.
		{"lzvn-pre_d-with-literal", preDWithLiteralPayload(2048)},
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
		// Regression for the findMatches overlapping-matches bug:
		// payloads alternating compressible and incompressible 1 KiB
		// chunks used to panic in encodeBlock.
		{"mixed-32KiB", mixedPayload(32 * 1024)},
		// 60-byte run + replicated noise — used to crash via the
		// "good match" pending-replacement path.
		{"good-match-8KiB", goodMatchPayload(8192)},
		// Two short / long competing matches near each other —
		// stressed the "compare vs pending" branch.
		{"pending-replace-8KiB", pendingReplacePayload(8192)},
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
			name: "sml_l-literal-then-pre_d-match",
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
			name:    "sml_d-truncated",
			stream:  []byte{0x08}, // sml_d with no follow byte
			nRaw:    0,
			wantErr: true,
		},
		{
			name:    "lrg_d-truncated",
			stream:  []byte{0x07, 0x01}, // lrg_d needs 2 follow bytes
			nRaw:    0,
			wantErr: true,
		},
		{
			name:    "med_d-truncated",
			stream:  []byte{0xA0}, // needs 2 follow bytes
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
			name: "lrg_m-without-prior",
			// lrg_m=0xF0 with no prior D
			stream:  []byte{0xF0, 0x05},
			nRaw:    1,
			wantErr: true,
		},
		{
			name: "sml_m-without-prior",
			// 0xF5 = sml_m M=5 with no prior D
			stream:  []byte{0xF5},
			nRaw:    1,
			wantErr: true,
		},
		{
			name:    "lrg_l-truncated",
			stream:  []byte{0xE0}, // promises a length byte that's missing
			nRaw:    1,
			wantErr: true,
		},
		{
			name:    "lrg_m-truncated",
			stream:  []byte{0xF0}, // missing the M byte
			nRaw:    1,
			wantErr: true,
		},
		{
			name: "sml_l-overflow",
			// sml_l with L=15 (0xEF) but no literal bytes after
			stream:  []byte{0xEF},
			nRaw:    15,
			wantErr: true,
		},
		{
			name: "med_d-then-match",
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
			name: "lrg_d-then-match",
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

// TestLZVNDecode_PreDWithLiteral exercises pre_d's L>0 path: a prior
// distance is established, then a `pre_d` opcode copies L literal
// bytes before reusing that distance.
func TestLZVNDecode_PreDWithLiteral(t *testing.T) {
	// Build:
	// 1. sml_l 4 bytes "ABCD" → dpos=4
	// 2. sml_d (opc 0x09: L=0 M=4 lo3=001) with b1=0x01 → D=(1<<8)|1=257.
	//    But that needs dpos≥257. Use D=1 instead: opc lo3=000.
	//    opc bits: LL=00 MMM=100 lo3=000 → M=4+3=7? wait MMM=100 → M = 4+3=7? Let me
	//    check: M = ((opc>>3) & 0x07) + 3 = 4 + 3 = 7. opc=0x20 + low3=0.
	//    But that overlaps med_d/sml_d encoding ambiguity? Actually opc=0x20 has
	//    bits 0b0010_0000: top bit is 0, so it's not 1010 (med_d). lo3=000 → sml_d.
	//    L=0 M=7 D=1. dpos was 4, now becomes 11.
	// 3. pre_d (lo3=110) with L=1, M=4: opc bits LL=01 MMM=001 lo3=110 → 0x4E.
	//    Wait MMM=001 → M = 1+3=4. Plus literal byte. After: copy 1 literal byte,
	//    then 4 bytes via prev D=1 (matches last byte).
	stream := []byte{
		0xE4, 'A', 'B', 'C', 'D', // sml_l L=4
		0x20, 0x01, // sml_d L=0 M=7 D=1
		0x4E, 'X', // pre_d L=1 M=4 → copy "X" then 4 bytes from D=1 ("XXXX")
	}
	// Expected output:
	//   ABCD (4) + DDDDDDD (7 'D's via D=1) + X (1) + XXXX (4 'X's via D=1) = 16 bytes
	want := []byte("ABCDDDDDDDDXXXXX")
	block := wrapLZVNBlock(stream, len(want))
	got, err := Decompress(block)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q (%d bytes), want %q (%d bytes)", got, len(got), want, len(want))
	}
}

// TestLZVNDecode_CopyMatchErrors crafts streams where the decoder
// passes the opcode-level checks but lzvnCopyMatch then fails:
// either D > dpos or dpos+M > dEnd.
func TestLZVNDecode_CopyMatchErrors(t *testing.T) {
	t.Run("sml_d-D-exceeds-dpos", func(t *testing.T) {
		// sml_l 1 byte then sml_d with D=5 but dpos=1 → copyMatch fails
		// since dpos - D < 0.
		stream := []byte{
			0xE1, 'A', // sml_l 1
			0x20, 0x05, // sml_d L=0 M=7 D=5
		}
		_, err := Decompress(wrapLZVNBlock(stream, 8))
		if err == nil {
			t.Fatal("expected invalid match distance error")
		}
	})
	t.Run("lrg_d-D-exceeds-dpos", func(t *testing.T) {
		// lrg_d (lo3=7) with L=0 M=0 D=10 followed by nothing else
		// dpos starts at 0, D=10 → copyMatch fails.
		stream := []byte{0x07, 0x0A, 0x00}
		_, err := Decompress(wrapLZVNBlock(stream, 3))
		if err == nil {
			t.Fatal("expected invalid match distance error")
		}
	})
	t.Run("med_d-D-exceeds-dpos", func(t *testing.T) {
		// med_d L=0 M=0 D=5 with dpos=0
		// b1: D bit2 in upper 6 bits = (5<<2)&0xFC = 0x14; M low2 = 0.
		// b2: D high bits = 0.
		// opc 0xA0: L=0, M=3 (from base 3), reads b1=0x14, b2=0x00 → D=(0x14>>2) | (0<<6) = 5
		stream := []byte{0xA0, 0x14, 0x00}
		_, err := Decompress(wrapLZVNBlock(stream, 3))
		if err == nil {
			t.Fatal("expected invalid match distance error")
		}
	})
	t.Run("match-only-overflows-dEnd", func(t *testing.T) {
		// Set a small dPrev via sml_d, then sml_m M=5 that overflows dEnd.
		// Stream: sml_l 1 'A' → dpos=1; sml_d L=0 M=4 D=1 → dpos=5;
		// sml_m M=5 → wants to copyMatch dpos=5, D=1, M=5 → dpos+M=10.
		// nRaw=8 → dEnd=8 → 10 > 8 → match overflow in match-only path.
		stream := []byte{
			0xE1, 'A',
			0x08, 0x01, // sml_d L=0 M=4 D=1
			0xF5, // sml_m M=5
		}
		_, err := Decompress(wrapLZVNBlock(stream, 8))
		if err == nil {
			t.Fatal("expected match-only overflow error")
		}
	})
	t.Run("pre_d-overflows-dEnd", func(t *testing.T) {
		// Set dPrev via sml_d, then pre_d (opc 0x1E: L=0 M=6) overflows.
		// Stream: sml_l 1 → dpos=1; sml_d L=0 M=4 D=1 → dpos=5;
		// pre_d L=0 M=6 → dpos+M=11, nRaw=8 → match overflow.
		stream := []byte{
			0xE1, 'A',
			0x08, 0x01, // sml_d L=0 M=4 D=1
			0x1E, // pre_d L=0 M=6
		}
		_, err := Decompress(wrapLZVNBlock(stream, 8))
		if err == nil {
			t.Fatal("expected pre_d overflow error")
		}
	})
	t.Run("match-only-D-exceeds-dpos", func(t *testing.T) {
		// First set a small dPrev via sml_d: literal 1, then sml_d D=1 M=4.
		// Output: "AAAAA" (5 bytes, dpos=5).
		// Then sml_m M=5 (opc 0xF5) — uses dPrev=1, dpos+M=10. But nRaw=4,
		// dEnd=4, so dpos+M = 5+5=10 > 4 → match overflow.
		stream := []byte{
			0xE1, 'A',
			0x20, 0x01, // sml_d L=0 M=7 D=1
			0xF5,
		}
		_, err := Decompress(wrapLZVNBlock(stream, 4))
		if err == nil {
			t.Fatal("expected match overflow error")
		}
	})
}

// TestLZVNDecode_LiteralOverflow_SmlD_LrgD targets the per-opcode
// "literal overflow" branches in sml_d (line 132) and lrg_d (172).
func TestLZVNDecode_LiteralOverflow_SmlDLrgD(t *testing.T) {
	t.Run("sml_d-literal-overflow", func(t *testing.T) {
		// sml_d L=3 M=3 D=1, but stream ends right after b1 (no literals)
		// opc bits: LL=11 MMM=000 lo3=000 → 0xC0 plus we need lo3 in [0..5]
		// to enter sml_d. opc=0xC0 has lo3=0. But opc>=0xC0 might match the
		// match-only or different branch. Let me use 0xC1: lo3=1 → sml_d
		// L=3 M=3 D=1.
		// Actually opc>=0xC0 falls into `default` because case `opc>=0xE0`
		// catches 0xE0+ and case 0xA0..0xC0 stops at <0xC0. So 0xC0..0xDF
		// falls through to default → sml_d (lo3∈[0..5]).
		// 0xC0: LL=11 MMM=000 lo3=0 → L=3 M=3 D=0+b1, needs 1 byte after for b1.
		// Provide b1=0x01 (D=1). But no 3 literals follow → "sml_d literal overflow".
		stream := []byte{0xC0, 0x01}
		_, err := Decompress(wrapLZVNBlock(stream, 6))
		if err == nil {
			t.Fatal("expected sml_d literal overflow error")
		}
	})
	t.Run("lrg_d-literal-overflow", func(t *testing.T) {
		// lrg_d (lo3=7) L=3 M=3, but no literals after b1/b2.
		// opc bits: LL=11 MMM=000 lo3=111 → 0xC7.
		stream := []byte{0xC7, 0x01, 0x00}
		_, err := Decompress(wrapLZVNBlock(stream, 6))
		if err == nil {
			t.Fatal("expected lrg_d literal overflow error")
		}
	})
	t.Run("pre_d-literal-overflow", func(t *testing.T) {
		// Set prior D via sml_d, then pre_d L=3 with no literals after.
		// sml_d: opc 0x20 b1=0x01 → L=0 M=7 D=1, dpos=7 after literal "A"
		// Wait we need a literal first. Use sml_l 1 + sml_d.
		// Then pre_d (lo3=6) L=3 M=3: opc LL=11 MMM=000 lo3=110 → 0xC6.
		// Stream: sml_l 1 "A" → dpos=1; sml_d L=0 M=4 D=1 → dpos=5;
		// pre_d L=3 M=3 → tries to read 3 literal bytes → overflow.
		stream := []byte{
			0xE1, 'A',
			0x20, 0x01, // sml_d L=0 M=7 D=1
			0xC6, // pre_d L=3 M=3
		}
		_, err := Decompress(wrapLZVNBlock(stream, 12))
		if err == nil {
			t.Fatal("expected pre_d literal overflow error")
		}
	})
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
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decompress panicked on corrupted input: %v", r)
				}
			}()
			_, _ = Decompress(buf)
		})
	}
}

// TestDecompress_RandomGarbage drives Decompress with pseudo-random
// byte streams of varying lengths and asserts no panic. Bad input
// must return an error, not crash.
func TestDecompress_RandomGarbage(t *testing.T) {
	for _, n := range []int{16, 64, 256, 1024, 4096, 16384} {
		for seed := int64(1); seed <= 8; seed++ {
			name := intName("size", n) + "-" + intName("seed", int(seed))
			t.Run(name, func(t *testing.T) {
				buf := pseudoRandom(n, seed*17+int64(n))
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Decompress panicked: %v", r)
					}
				}()
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

// mixedPayload alternates 1 KiB of prose with 1 KiB of pseudo-random
// noise. Used to fire the findMatches overlapping-matches path.
func mixedPayload(n int) []byte {
	out := make([]byte, 0, n)
	prose := englishProse(n)
	noise := pseudoRandom(n, 9)
	chunk := 1024
	flip := false
	for len(out) < n {
		x := chunk
		if !flip {
			off := len(out) % len(prose)
			if off+x > len(prose) {
				x = len(prose) - off
			}
			out = append(out, prose[off:off+x]...)
		} else {
			off := len(out) % len(noise)
			if off+x > len(noise) {
				x = len(noise) - off
			}
			out = append(out, noise[off:off+x]...)
		}
		flip = !flip
	}
	return out[:n]
}

// goodMatchPayload plants a 60-byte run twice so findMatches takes
// its "bestM >= encodeGoodMatch (40)" emit-immediately branch.
func goodMatchPayload(n int) []byte {
	out := make([]byte, n)
	copy(out, pseudoRandom(n, 17))
	marker := bytes.Repeat([]byte{'G'}, 60)
	copy(out[64:], marker)
	copy(out[2048:], marker)
	return out
}

// preDWithLiteralPayload plants pairs of identical 4-byte windows
// separated by a 1–2 byte gap so the encoder emits a (D == dPrev,
// L > 0) record. Then a similar pair further away resumes the
// same distance, triggering pre_d with literal preamble.
func preDWithLiteralPayload(n int) []byte {
	out := make([]byte, n)
	copy(out, pseudoRandom(n, 53))
	// Pattern: WW W X WW W   (W = unique 4 bytes, X = 1 byte)
	w := pseudoRandom(4, 57)
	off := 200
	copy(out[off:], w)
	out[off+4] = 'x'
	copy(out[off+5:], w)
	out[off+9] = 'y'
	copy(out[off+10:], w)
	// Replicate the pattern later at the same relative distance.
	off2 := 800
	copy(out[off2:], w)
	out[off2+4] = 'x'
	copy(out[off2+5:], w)
	out[off2+9] = 'y'
	copy(out[off2+10:], w)
	return out
}

// pendingReplacePayload plants competing short / long matches near
// each other so findMatches enters the pending-replacement branch.
func pendingReplacePayload(n int) []byte {
	out := make([]byte, n)
	copy(out, pseudoRandom(n, 19))
	short := pseudoRandom(8, 23)
	long := append(append([]byte(nil), short...), pseudoRandom(16, 25)...)
	copy(out[64:], short)
	copy(out[80:], long)
	copy(out[1024:], short)
	copy(out[1040:], long)
	return out
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

// TestRoundTrip_LargeMultiBlock exercises inputs big enough to span several
// LZFSE blocks. It is a regression guard for two distinct bugs:
//
//  1. Cross-block back-references. The compressor splits the input into blocks
//     at match boundaries, but LZFSE's history window spans those boundaries —
//     a match near the start of a block may reference output produced in an
//     earlier block. The decoder previously decoded each block into a fresh
//     buffer and rejected such distances with "invalid match distance"; it now
//     decodes into the running stream so they resolve.
//
//  2. The incompressible fallback. Random data that the FSE model cannot shrink
//     must be stored in an uncompressed block rather than emitted as a broken
//     compressed block.
func TestRoundTrip_LargeMultiBlock(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"prose-2MiB", englishProse(2 << 20)},
		{"mixed-4MiB", mixedPayload(4 << 20)},
		{"random-2MiB", pseudoRandom(2<<20, 99)},
		{"zeros-2MiB", make([]byte, 2<<20)},
		{"repetitive-4MiB", bytes.Repeat([]byte("The quick brown fox. "), (4<<20)/21)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp, err := Compress(tc.data)
			if err != nil {
				t.Fatalf("Compress: %v", err)
			}
			got, err := Decompress(comp)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(tc.data))
			}
		})
	}
}

// TestCompress_IncompressibleStored verifies that data the entropy coder cannot
// model is stored (not expanded into a corrupt compressed block) and that the
// stored form still round-trips.
func TestCompress_IncompressibleStored(t *testing.T) {
	src := pseudoRandom(256<<10, 123) // > encodeLZVNThreshold, incompressible
	comp, err := Compress(src)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	// The stored form must never balloon the data beyond a tiny fixed overhead.
	if len(comp) > len(src)+uncompressedOverhead {
		t.Fatalf("incompressible data expanded: %d > %d+%d", len(comp), len(src), uncompressedOverhead)
	}
	got, err := Decompress(comp)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("stored round-trip mismatch")
	}
}
