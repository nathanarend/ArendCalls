package mlow

import (
	"math"
	"sync"
	"sync/atomic"
)

// cpx is a single-precision complex value.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L318-L343
type cpx struct {
	re, im float32
}

func (a cpx) add(b cpx) cpx {
	return cpx{re: a.re + b.re, im: a.im + b.im}
}

func (a cpx) mul(b cpx) cpx {
	return cpx{
		re: a.re*b.re - a.im*b.im,
		im: a.re*b.im + a.im*b.re,
	}
}

// twiddleCache memoizes, per transform length n, the n-th roots of unity
//
//	w[m] = exp(±i·2π·m/n)   (index 0 = forward exp(-i…), index 1 = inverse)
//
// fftRec used to recompute these with math.Cos/math.Sin inside its inner loops on
// every call — profiling put math.Cos alone at ~45% of the whole encoder. Only n
// distinct values are ever needed (the angle is 2π·(a·b mod n)/n), and only a
// handful of lengths occur in practice (512 for LPC, 576 for the perceptual
// model, plus the radix-split sub-lengths), so after warm-up this is pure
// read-only lookups. Computing the table at 2π·m/n in float64 also keeps the
// angle in [0, 2π) instead of letting math.Cos see a large, precision-losing
// float32 argument for high bins — closer to the reference FFT, not further.
//
// Stored as a copy-on-write map behind an atomic pointer: the steady-state read
// is an atomic load + int-keyed map lookup, no lock and no interface hashing (a
// sync.Map keyed by a struct showed up at ~10% of encoder CPU in typehash +
// HashTrieMap.Load). Writes happen only until every n has been seen once.
var (
	twiddleCache atomic.Pointer[map[int]*[2][]cpx]
	twiddleMu    sync.Mutex
)

func buildTwiddles(n int, sign float64) []cpx {
	t := make([]cpx, n)
	for m := 0; m < n; m++ {
		s, c := math.Sincos(sign * 2.0 * math.Pi * float64(m) / float64(n))
		t[m] = cpx{re: float32(c), im: float32(s)}
	}
	return t
}

func twiddles(n int, forward bool) []cpx {
	idx := 0
	if !forward {
		idx = 1
	}
	if m := twiddleCache.Load(); m != nil {
		if e := (*m)[n]; e != nil {
			return e[idx]
		}
	}
	twiddleMu.Lock()
	defer twiddleMu.Unlock()
	old := twiddleCache.Load()
	if old != nil {
		if e := (*old)[n]; e != nil {
			return e[idx]
		}
	}
	next := make(map[int]*[2][]cpx)
	if old != nil {
		for k, v := range *old {
			next[k] = v
		}
	}
	entry := &[2][]cpx{buildTwiddles(n, -1.0), buildTwiddles(n, 1.0)}
	next[n] = entry
	twiddleCache.Store(&next)
	return entry[idx]
}

// smallestFactor returns the smallest prime factor of n (>= 2).
func smallestFactor(n int) int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L346-L358
	if n%2 == 0 {
		return 2
	}
	p := 3
	for p*p <= n {
		if n%p == 0 {
			return p
		}
		p += 2
	}
	return n
}

// Global buffer pool to eliminate heap allocations in the FFT hot path.
var cpxScratchPool = sync.Pool{
	New: func() any {
		b := make([]cpx, 4096)
		return &b
	},
}

func getCpxScratch(needed int) ([]cpx, func()) {
	ptr := cpxScratchPool.Get().(*[]cpx)
	if len(*ptr) < needed {
		*ptr = make([]cpx, needed*2)
	}
	slice := (*ptr)[:needed]
	return slice, func() {
		cpxScratchPool.Put(ptr)
	}
}

// fftRec is the recursive mixed-radix Cooley-Tukey DFT using scratch space without allocations.
func fftRec(x []cpx, stride, n int, sign float32, out []cpx, scratch []cpx) {
	if n == 1 {
		out[0] = x[0]
		return
	}
	p := smallestFactor(n)
	tw := twiddles(n, sign < 0) // w[m] = exp(±i·2π·m/n), indexed by (a·b) mod n
	if p == n {
		for k := 0; k < n; k++ {
			var acc cpx
			mi := 0 // (k·j) mod n, advanced by k each step
			for j := 0; j < n; j++ {
				acc = acc.add(x[j*stride].mul(tw[mi]))
				if mi += k; mi >= n {
					mi -= n
				}
			}
			out[k] = acc
		}
		return
	}
	m := n / p
	sub := scratch[:n]
	nextScratch := scratch[n:]
	for q := 0; q < p; q++ {
		fftRec(x[q*stride:], stride*p, m, sign, sub[q*m:(q+1)*m], nextScratch)
	}
	for k := 0; k < n; k++ {
		kmod := k % m
		var acc cpx
		mi := 0 // (k·q) mod n, advanced by k each step
		for q := 0; q < p; q++ {
			acc = acc.add(sub[q*m+kmod].mul(tw[mi]))
			if mi += k; mi >= n {
				mi -= n
			}
		}
		out[k] = acc
	}
}

// cfft computes the complex FFT of a mixed-radix length into out. sign=-1 forward, +1 inverse.
func cfft(input, out []cpx, sign float32) {
	n := len(input)
	scratch, release := getCpxScratch(n * 6)
	defer release()
	fftRec(input, 1, n, sign, out, scratch)
}

// rfftForwardOrdered is the forward real FFT of n real samples, re-packed into the
// ordered REAL layout: f[0]=DC.re, f[1]=Nyquist.re, then [re,im] pairs for bins
// 1..n/2-1. Output length is n.
func rfftForwardOrdered(time, f []float32) {
	n := len(time)
	scratch, release := getCpxScratch(n * 8)
	defer release()

	cin := scratch[:n]
	spec := scratch[n : 2*n]
	recScratch := scratch[2*n:]

	for i := 0; i < n; i++ {
		cin[i].re = time[i]
		cin[i].im = 0
	}
	fftRec(cin, 1, n, -1.0, spec, recScratch)
	f[0] = spec[0].re
	f[1] = spec[n/2].re
	for i := 1; i < n/2; i++ {
		f[2*i] = spec[i].re
		f[2*i+1] = spec[i].im
	}
}
