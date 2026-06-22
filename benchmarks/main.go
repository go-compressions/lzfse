// Command bench measures go-compressions/lzfse against Apple's reference
// lzfse C implementation on the same machine: compression ratio, single-core
// compression throughput, and single-core decompression throughput.
//
// It is deliberately a standalone module (see go.mod) so it stays isolated
// from the library's 100% coverage gate.
//
// Reference numbers come from an in-memory C harness (ref/lzfse_bench.c) linked
// against liblzfse, so both sides are timed the same way: pure in-process
// encode/decode of a buffer already resident in memory, no file or process I/O.
//
// Usage:
//
//	go run . -corpus <dir> -ref <path-to-lzfse_bench> -iters 21
//
// The corpus directory is populated by ./fetch_corpus.sh (Silesia + synthetic).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-compressions/lzfse"
)

func main() {
	corpus := flag.String("corpus", "corpus", "directory of corpus files")
	refBin := flag.String("ref", "", "path to the compiled C reference harness (lzfse_bench); empty = skip reference")
	iters := flag.Int("iters", 21, "timed iterations per file (best+median reported)")
	flag.Parse()

	files, err := listCorpus(*corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}

	fmt.Printf("# lzfse parity run  (%s)\n", time.Now().Format("2006-01-02"))
	fmt.Printf("%-16s %12s %12s %12s %12s %10s %10s %s\n",
		"file", "ourComp", "refComp", "ourDecomp", "refDecomp", "ourRatio", "refRatio", "rt")
	fmt.Printf("%-16s %12s %12s %12s %12s %10s %10s %s\n",
		"", "MB/s", "MB/s", "MB/s", "MB/s", "", "", "")

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			continue
		}
		name := filepath.Base(f)
		r := benchOurs(src, *iters)
		var ref refResult
		haveRef := false
		if *refBin != "" {
			ref, err = benchRef(*refBin, f, *iters)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ref %s: %v\n", name, err)
			} else {
				haveRef = true
			}
		}
		refC, refD, refR := "-", "-", "-"
		if haveRef {
			refC = fmt.Sprintf("%.1f", ref.compMBps)
			refD = fmt.Sprintf("%.1f", ref.decompMBps)
			refR = fmt.Sprintf("%.3f", ref.ratio)
		}
		fmt.Printf("%-16s %12.1f %12s %12.1f %12s %10.3f %10s %s\n",
			name, r.compMBps, refC, r.decompMBps, refD, r.ratio, refR, boolMark(r.roundTrip))
	}
}

type ourResult struct {
	compMBps   float64
	decompMBps float64
	ratio      float64
	roundTrip  bool
}

func benchOurs(src []byte, iters int) ourResult {
	// Warm up + learn compressed form.
	comp, err := lzfse.Compress(src)
	if err != nil {
		return ourResult{}
	}
	dec, err := lzfse.Decompress(comp)
	rt := err == nil && len(dec) == len(src) && bytesEqual(dec, src)
	for i := 0; i < 3; i++ {
		_, _ = lzfse.Compress(src)
		_, _ = lzfse.Decompress(comp)
	}

	cTimes := make([]time.Duration, iters)
	for i := range cTimes {
		t0 := time.Now()
		_, _ = lzfse.Compress(src)
		cTimes[i] = time.Since(t0)
	}
	dTimes := make([]time.Duration, iters)
	for i := range dTimes {
		t0 := time.Now()
		_, _ = lzfse.Decompress(comp)
		dTimes[i] = time.Since(t0)
	}
	return ourResult{
		compMBps:   mbps(len(src), best(cTimes)),
		decompMBps: mbps(len(src), best(dTimes)),
		ratio:      float64(len(comp)) / float64(len(src)),
		roundTrip:  rt,
	}
}

type refResult struct {
	compMBps   float64
	decompMBps float64
	ratio      float64
}

// benchRef runs the C harness, which prints:
// origbytes compbytes comp_ns_best decomp_ns_best comp_ns_med decomp_ns_med
func benchRef(bin, file string, iters int) (refResult, error) {
	out, err := exec.Command(bin, file, strconv.Itoa(iters)).Output()
	if err != nil {
		return refResult{}, err
	}
	f := strings.Fields(strings.TrimSpace(string(out)))
	if len(f) < 6 {
		return refResult{}, fmt.Errorf("unexpected ref output: %q", out)
	}
	orig, _ := strconv.Atoi(f[0])
	comp, _ := strconv.Atoi(f[1])
	cBest, _ := strconv.ParseInt(f[2], 10, 64)
	dBest, _ := strconv.ParseInt(f[3], 10, 64)
	return refResult{
		compMBps:   mbps(orig, time.Duration(cBest)),
		decompMBps: mbps(orig, time.Duration(dBest)),
		ratio:      float64(comp) / float64(orig),
	}, nil
}

func listCorpus(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasPrefix(filepath.Base(p), ".") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func mbps(nbytes int, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return (float64(nbytes) / 1e6) / d.Seconds()
}

func best(ts []time.Duration) time.Duration {
	m := ts[0]
	for _, t := range ts {
		if t < m {
			m = t
		}
	}
	return m
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func boolMark(b bool) string {
	if b {
		return "ok"
	}
	return "FAIL"
}
