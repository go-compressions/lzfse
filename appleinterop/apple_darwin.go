//go:build darwin && cgo

package appleinterop

// The cgo call into libcompression moved to go-compressions/appleoracle, so
// these four are now relays. They keep their signatures exactly, because the
// test file around them is careful about what each size means and this change
// is meant to move code, not to change a verdict.
//
// ⛔ appleLZFSEEncode and appleLZVNEncode still return an empty slice when the
// platform declines, which is what they did when they wrapped
// compression_encode_buffer directly. appleoracle reports that explicitly and
// a NEW caller should use appleoracle.Encode's ok -- see the package doc
// there. It is preserved here rather than fixed in the same commit so that a
// green run afterwards means "the same thing, from one place" and nothing else.

import "github.com/go-compressions/appleoracle"

func appleLZFSEEncode(src []byte) []byte {
	out, _ := appleoracle.Encode(appleoracle.LZFSE, src)
	return out
}

func appleLZFSEDecode(src []byte, expect int) (out []byte, ok bool) {
	return appleoracle.Decode(appleoracle.LZFSE, src, expect)
}

func appleLZVNEncode(src []byte) []byte {
	out, _ := appleoracle.Encode(appleoracle.LZVN, src)
	return out
}

func appleLZVNDecode(src []byte, expect int) (out []byte, ok bool) {
	return appleoracle.Decode(appleoracle.LZVN, src, expect)
}
