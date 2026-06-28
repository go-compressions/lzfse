//go:build darwin && cgo

package appleinterop

/*
#cgo LDFLAGS: -lcompression
#include <stdlib.h>
#include <compression.h>

static size_t apple_lzfse_encode(unsigned char *dst, size_t dstcap,
                                 const unsigned char *src, size_t n) {
    return compression_encode_buffer(dst, dstcap, src, n, NULL, COMPRESSION_LZFSE);
}
static size_t apple_lzfse_decode(unsigned char *dst, size_t dstcap,
                                 const unsigned char *src, size_t n) {
    return compression_decode_buffer(dst, dstcap, src, n, NULL, COMPRESSION_LZFSE);
}
*/
import "C"

import "unsafe"

// appleLZFSEEncode compresses src with Apple's system libcompression
// (COMPRESSION_LZFSE) and returns the raw LZFSE stream.
func appleLZFSEEncode(src []byte) []byte {
	dstcap := len(src) + 4096
	dst := make([]byte, dstcap)
	var sp *C.uchar
	if len(src) > 0 {
		sp = (*C.uchar)(unsafe.Pointer(&src[0]))
	}
	n := C.apple_lzfse_encode((*C.uchar)(unsafe.Pointer(&dst[0])), C.size_t(dstcap), sp, C.size_t(len(src)))
	return dst[:n]
}

// appleLZFSEDecode decompresses an LZFSE stream with Apple's system
// libcompression. ok reports whether exactly expect bytes were produced.
func appleLZFSEDecode(src []byte, expect int) (out []byte, ok bool) {
	dstcap := expect + 64
	dst := make([]byte, dstcap)
	var sp *C.uchar
	if len(src) > 0 {
		sp = (*C.uchar)(unsafe.Pointer(&src[0]))
	}
	n := C.apple_lzfse_decode((*C.uchar)(unsafe.Pointer(&dst[0])), C.size_t(dstcap), sp, C.size_t(len(src)))
	return dst[:n], int(n) == expect
}
