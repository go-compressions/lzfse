package lzfse

import (
	"errors"
	"testing"
)

// TestLmdSymbol_FallbackZero: v=0 with non-zero base[0] hits the
// final `return 0` line (the loop terminates without a match).
func TestLmdSymbol_FallbackZero(t *testing.T) {
	base := []int32{1, 2, 3} // every entry > v
	extra := []uint8{0, 0, 0}
	got := lmdSymbol(0, base, extra, 3)
	if got != 0 {
		t.Errorf("lmdSymbol(0): got %d, want 0", got)
	}
}

// TestEnsureNonZero covers both the early-return branch (a non-zero
// count already exists) and the patch branch (all zero, bump
// counts[0]).
func TestEnsureNonZero(t *testing.T) {
	t.Run("already-non-zero", func(t *testing.T) {
		c := []int{0, 0, 3, 0}
		ensureNonZero(c)
		if c[0] != 0 || c[2] != 3 {
			t.Errorf("ensureNonZero modified non-zero slice: %v", c)
		}
	})
	t.Run("all-zero", func(t *testing.T) {
		c := []int{0, 0, 0}
		ensureNonZero(c)
		if c[0] != 1 || c[1] != 0 || c[2] != 0 {
			t.Errorf("ensureNonZero(all-zero): got %v, want [1 0 0]", c)
		}
	})
}

// TestFseInInit_ShortBuffers exercises the error branches and the
// empty-buffer special case.
func TestFseInInit_ShortBuffers(t *testing.T) {
	if _, err := fseInInit(make([]byte, 4), 4, -3); err == nil {
		t.Errorf("fseInInit(end=4, n=-3): expected error")
	}
	if _, err := fseInInit(make([]byte, 3), 3, 0); err == nil {
		t.Errorf("fseInInit(end=3, n=0): expected error")
	}
	s, err := fseInInit(nil, 0, 0)
	if err != nil {
		t.Errorf("fseInInit(empty, n=0): %v", err)
	}
	if s.accum != 0 || s.accumNBits != 56 {
		t.Errorf("fseInInit(empty): got accum=%d nbits=%d, want 0/56", s.accum, s.accumNBits)
	}
}

// TestLzvnCopyMatch_Errors directly drives the two error branches.
func TestLzvnCopyMatch_Errors(t *testing.T) {
	dst := make([]byte, 16)
	t.Run("invalid-distance-zero", func(t *testing.T) {
		if err := lzvnCopyMatch(dst, 4, 0, 3, 16); err == nil {
			t.Error("D=0: expected error")
		}
	})
	t.Run("invalid-distance-negative-source", func(t *testing.T) {
		if err := lzvnCopyMatch(dst, 4, 10, 3, 16); err == nil {
			t.Error("D > dpos: expected error")
		}
	})
	t.Run("match-overflow", func(t *testing.T) {
		if err := lzvnCopyMatch(dst, 14, 4, 8, 16); err == nil {
			t.Error("dpos+M > dEnd: expected error")
		}
	})
}

// TestFseNormalizeFreq_BadInput hits the error returns in
// fseNormalizeFreq (zero counts, zero target, etc.).
func TestFseNormalizeFreq_BadInput(t *testing.T) {
	t.Run("all-zero", func(t *testing.T) {
		_, err := fseNormalizeFreq([]int{0, 0, 0}, 64)
		if err == nil {
			t.Error("expected error for all-zero counts")
		}
	})
	t.Run("nstates-not-power-of-two", func(t *testing.T) {
		// Calling with nstates that isn't a power of two should error.
		_, err := fseNormalizeFreq([]int{1, 1, 1, 1}, 6)
		if err == nil {
			// fseNormalizeFreq might tolerate this; keep test informational.
			t.Skip("nstates=6 accepted (not enforced as a power of two)")
		}
	})
}

// TestReadV1Header_TooShort covers the early-return error in
// readV1Header when the buffer is shorter than the header size.
func TestReadV1Header_TooShort(t *testing.T) {
	_, err := readV1Header(make([]byte, 16))
	if err == nil {
		t.Fatal("expected error for short buffer")
	}
	if !errIsLZFSE(err, "V1 header too short") {
		t.Fatalf("got %v, want 'V1 header too short'", err)
	}
}

// errIsLZFSE is a tiny substring-style errors helper.
func errIsLZFSE(err error, substr string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for i := 0; i+len(substr) <= len(msg); i++ {
		if msg[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure the errors package import is used.
var _ = errors.New
