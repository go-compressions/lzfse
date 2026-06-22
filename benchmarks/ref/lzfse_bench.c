// In-memory lzfse reference benchmark.
// Usage: lzfse_bench <file> <iters>
// Prints: origbytes compbytes comp_ns_best decomp_ns_best comp_ns_med decomp_ns_med
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>
#include "lzfse.h"

static uint64_t now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ull + ts.tv_nsec;
}

static int cmp_u64(const void *a, const void *b) {
    uint64_t x = *(const uint64_t*)a, y = *(const uint64_t*)b;
    return (x>y)-(x<y);
}

int main(int argc, char **argv) {
    if (argc < 3) { fprintf(stderr, "usage: %s file iters\n", argv[0]); return 2; }
    const char *path = argv[1];
    int iters = atoi(argv[2]);

    FILE *f = fopen(path, "rb");
    if (!f) { perror("open"); return 1; }
    fseek(f, 0, SEEK_END);
    long n = ftell(f);
    fseek(f, 0, SEEK_SET);
    uint8_t *src = malloc(n);
    if (fread(src, 1, n, f) != (size_t)n) { perror("read"); return 1; }
    fclose(f);

    size_t enc_scratch = lzfse_encode_scratch_size();
    size_t dec_scratch = lzfse_decode_scratch_size();
    void *escr = malloc(enc_scratch);
    void *dscr = malloc(dec_scratch);
    size_t dstcap = n + 4096;
    uint8_t *dst = malloc(dstcap);

    // one encode to learn compressed size
    size_t comp = lzfse_encode_buffer(dst, dstcap, src, n, escr);
    if (comp == 0) { fprintf(stderr, "encode failed\n"); return 1; }
    uint8_t *cbuf = malloc(comp);
    memcpy(cbuf, dst, comp);

    uint8_t *decbuf = malloc(n + 64);

    // warm up
    for (int i=0;i<3;i++) {
        lzfse_encode_buffer(dst, dstcap, src, n, escr);
        lzfse_decode_buffer(decbuf, n+64, cbuf, comp, dscr);
    }

    uint64_t *ce = malloc(iters*sizeof(uint64_t));
    uint64_t *de = malloc(iters*sizeof(uint64_t));
    for (int i=0;i<iters;i++){
        uint64_t t0 = now_ns();
        lzfse_encode_buffer(dst, dstcap, src, n, escr);
        ce[i] = now_ns()-t0;
    }
    for (int i=0;i<iters;i++){
        uint64_t t0 = now_ns();
        size_t dn = lzfse_decode_buffer(decbuf, n+64, cbuf, comp, dscr);
        de[i] = now_ns()-t0;
        if (dn != (size_t)n) { fprintf(stderr,"decode size mismatch\n"); return 1; }
    }
    if (memcmp(decbuf, src, n)!=0){ fprintf(stderr,"roundtrip mismatch\n"); return 1; }

    qsort(ce, iters, sizeof(uint64_t), cmp_u64);
    qsort(de, iters, sizeof(uint64_t), cmp_u64);
    printf("%ld %zu %llu %llu %llu %llu\n", n, comp,
        (unsigned long long)ce[0], (unsigned long long)de[0],
        (unsigned long long)ce[iters/2], (unsigned long long)de[iters/2]);
    return 0;
}
