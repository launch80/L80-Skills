// Package betterbench maps a BetterBench results.json onto the bench.report.v1
// template.
//
// The statistics here are a port of betterbench/metrics.py and the row
// derivations in betterbench/report.py (https://github.com/GGZ14/BetterBench),
// so a page published through `L80 betterbench` shows the same numbers the
// tool's own markdown report prints. Where a choice had to be made the
// comment says which BetterBench function it mirrors.
package betterbench

import (
	"math"
	"sort"
)

// Dist summarises one metric's distribution. Latencies are in ms, rates in
// tokens per second. Mirrors metrics.Dist; only the fields the template
// needs are carried.
type Dist struct {
	N      int
	Median float64
	P99    float64
	IQR    float64
	Low1   float64 // mean of the worst 1% (rate-like metrics only)
}

// percentile matches numpy.percentile's default "linear" method.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := (float64(n) - 1) * p / 100
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}

// summarize mirrors metrics.summarize. Non-finite samples are dropped; an
// empty set yields N == 0, and callers treat that as "no figure" rather than
// printing a zero the tool never measured.
func summarize(samples []float64, rateLike bool) Dist {
	a := make([]float64, 0, len(samples))
	for _, s := range samples {
		if !math.IsNaN(s) && !math.IsInf(s, 0) {
			a = append(a, s)
		}
	}
	if len(a) == 0 {
		return Dist{}
	}
	sort.Float64s(a)
	d := Dist{
		N:      len(a),
		Median: percentile(a, 50),
		P99:    percentile(a, 99),
		IQR:    percentile(a, 75) - percentile(a, 25),
	}
	if rateLike {
		// metrics._worst_tail_mean: k = max(1, int(round(n * 0.01))). Python's
		// round is banker's rounding, hence RoundToEven.
		k := int(math.RoundToEven(float64(len(a)) * 0.01))
		if k < 1 {
			k = 1
		}
		sum := 0.0
		for _, v := range a[:k] {
			sum += v
		}
		d.Low1 = sum / float64(k)
	}
	return d
}

// enoughSamplesForPercentile mirrors metrics.enough_samples_for_percentile:
// n * min(pct, 100-pct)/100 >= 5. A p99 needs 500 observations.
func enoughSamplesForPercentile(n int, pct float64) bool {
	tail := math.Min(pct, 100-pct) / 100
	return float64(n)*tail >= 5
}

// samplesNeeded is the n at which enoughSamplesForPercentile turns true.
func samplesNeeded(pct float64) int {
	tail := math.Min(pct, 100-pct) / 100
	return int(math.Ceil(5 / tail))
}

// itlToRateSamples mirrors metrics.itl_to_rate_samples: per-token gaps in ms
// become instantaneous tokens/s; zero and negative gaps are dropped.
func itlToRateSamples(gapsMs []float64) []float64 {
	out := make([]float64, 0, len(gapsMs))
	for _, g := range gapsMs {
		if g > 0 {
			out = append(out, 1000.0/g)
		}
	}
	return out
}

// Chunking classification constants, from betterbench/client.py.
const (
	chunkTokenTol   = 0.02
	chunkTokenSlack = 2
)

// classifyChunking mirrors client.classify_chunking: did the server stream
// one token per update, or several? "unknown" means there is no trustworthy
// token count to divide by, and the tool refuses to guess 1:1.
func classifyChunking(completionTokens, nChunks int) string {
	if completionTokens == 0 || nChunks <= 1 {
		return "unknown"
	}
	tpu := float64(completionTokens) / float64(nChunks)
	if tpu < 0.90 {
		return "unknown"
	}
	if float64(completionTokens-nChunks) <= math.Max(chunkTokenSlack, chunkTokenTol*float64(nChunks)) {
		return "per_token"
	}
	return "batched"
}
