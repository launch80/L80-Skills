package betterbench

import (
	"bytes"
	"io"
	"sort"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
func sortStrings(s []string)         { sort.Strings(s) }

// runGaps mirrors report.run_gaps_ms.
func runGaps(r Run) []float64 {
	if r.UpdateGapsMs != nil {
		return *r.UpdateGapsMs
	}
	comp := 0
	if r.CompletionTokens != nil {
		comp = *r.CompletionTokens
	}
	if len(r.ITLMs) > 0 && comp > 0 && r.NChunks > 0 {
		scale := float64(comp) / float64(r.NChunks)
		out := make([]float64, len(r.ITLMs))
		for i, x := range r.ITLMs {
			out[i] = x * scale
		}
		return out
	}
	return r.ITLMs
}

// runChunking mirrors report.run_chunking: re-derived from the counts, with
// the stored flag only as a fallback when no counts exist.
func runChunking(r Run) string {
	comp := 0
	if r.CompletionTokens != nil {
		comp = *r.CompletionTokens
	}
	if comp > 0 && r.NChunks > 0 {
		return classifyChunking(comp, r.NChunks)
	}
	if r.ChunkTokenMismatch {
		return "batched"
	}
	return "unknown"
}

// CategoryRow mirrors report._category_row. A nil pointer means "not
// measured", which the payload leaves out so the column disappears.
type CategoryRow struct {
	Name         string
	Runs         int
	Failed       int
	Truncated    int
	TTFTN, GapN  int
	TTFTP99OK    bool
	TailOK       bool
	TTFTP50      *float64
	TTFTP99      *float64
	UpdateP50    *float64
	UpdateP99    *float64
	TokPerUpdate *float64
	BatchedRuns  int
	Batched      bool
	DecodeMed    *float64
	DecodeIQR    *float64
	ITLLow1      *float64
	ITLMed       *float64
	ITLHigh99    *float64
}

func f(v float64) *float64 { return &v }

func categoryRow(name string, recs []Run) CategoryRow {
	var ttft, dtps, gaps []float64
	var comp, chunks, batched, failed, truncated int
	nOK := 0
	for _, r := range recs {
		if !r.OK {
			failed++
			continue
		}
		nOK++
		if r.TTFTMs != nil && *r.TTFTMs != 0 {
			ttft = append(ttft, *r.TTFTMs)
		}
		if r.DecodeTPS != nil && *r.DecodeTPS != 0 {
			dtps = append(dtps, *r.DecodeTPS)
		}
		gaps = append(gaps, runGaps(r)...)
		if r.CompletionTokens != nil {
			comp += *r.CompletionTokens
		}
		chunks += r.NChunks
		if runChunking(r) == "batched" {
			batched++
		}
		if r.FinishReason != nil && *r.FinishReason == "length" {
			truncated++
		}
	}
	ttftD, dtpsD, gapD := summarize(ttft, false), summarize(dtps, false), summarize(gaps, false)

	row := CategoryRow{
		Name: name, Runs: nOK, Failed: failed, Truncated: truncated,
		TTFTN: ttftD.N, GapN: gapD.N,
		TTFTP99OK:   enoughSamplesForPercentile(ttftD.N, 99),
		TailOK:      enoughSamplesForPercentile(gapD.N, 99),
		BatchedRuns: batched, Batched: batched > 0,
	}
	if ttftD.N > 0 {
		row.TTFTP50, row.TTFTP99 = f(ttftD.Median), f(ttftD.P99)
	}
	if dtpsD.N > 0 {
		row.DecodeMed, row.DecodeIQR = f(dtpsD.Median), f(dtpsD.IQR)
	}
	if gapD.N > 0 {
		row.UpdateP50, row.UpdateP99 = f(gapD.Median), f(gapD.P99)
	}
	if chunks > 0 {
		row.TokPerUpdate = f(float64(comp) / float64(chunks))
	}
	// Per-token ITL exists only when the server streams one token per chunk.
	if batched == 0 && gapD.N > 0 {
		rateD := summarize(itlToRateSamples(gaps), true)
		if rateD.N > 0 {
			row.ITLLow1, row.ITLMed, row.ITLHigh99 = f(rateD.Low1), f(rateD.Median), f(rateD.P99)
		}
	}
	return row
}

// SingleRows mirrors report.single_rows, in file order.
func (r *Results) SingleRows() []CategoryRow {
	cats := r.Categories()
	rows := make([]CategoryRow, 0, len(cats))
	for _, c := range cats {
		rows = append(rows, categoryRow(c, r.SingleStream[c]))
	}
	return rows
}

// reportIsBatched mirrors report.report_is_batched.
func reportIsBatched(rows []CategoryRow) bool {
	for _, r := range rows {
		if r.Batched {
			return true
		}
	}
	return false
}

type ConcurrencyRow struct {
	Level        int
	OK, Requests int
	AggregateTPS float64
	TTFTN        int
	TTFTP99OK    bool
	TTFTP50      *float64
	TTFTP99      *float64
	DecodeMed    *float64
}

// ConcurrencyRows mirrors report.concurrency_rows.
func (r *Results) ConcurrencyRows() []ConcurrencyRow {
	rows := make([]ConcurrencyRow, 0, len(r.Concurrency))
	for _, s := range r.Concurrency {
		td, dd := summarize(s.TTFTMs, false), summarize(s.DecodeTPS, false)
		row := ConcurrencyRow{
			Level: s.Level, OK: s.OK, Requests: s.Requests, AggregateTPS: s.AggregateTPS,
			TTFTN: td.N, TTFTP99OK: enoughSamplesForPercentile(td.N, 99),
		}
		if td.N > 0 {
			row.TTFTP50, row.TTFTP99 = f(td.Median), f(td.P99)
		}
		if dd.N > 0 {
			row.DecodeMed = f(dd.Median)
		}
		rows = append(rows, row)
	}
	return rows
}

type PrefillRow struct {
	TargetDepth int
	Skipped     bool
	Reason      string
	PPN         int
	PPTailOK    bool
	TTFTP50     *float64
	PPMed       *float64
	PPLow1      *float64
	PPP99       *float64
}

// PrefillRows mirrors report.prefill_rows.
func (r *Results) PrefillRows() []PrefillRow {
	rows := make([]PrefillRow, 0, len(r.Prefill))
	for _, d := range r.Prefill {
		if d.Skipped {
			rows = append(rows, PrefillRow{TargetDepth: d.TargetDepth, Skipped: true, Reason: d.Reason})
			continue
		}
		td, pp := summarize(d.TTFTMs, false), summarize(d.PPTPS, true)
		row := PrefillRow{
			TargetDepth: d.TargetDepth, PPN: pp.N,
			PPTailOK: enoughSamplesForPercentile(pp.N, 99),
		}
		if td.N > 0 {
			row.TTFTP50 = f(td.Median)
		}
		if pp.N > 0 {
			row.PPMed, row.PPLow1, row.PPP99 = f(pp.Median), f(pp.Low1), f(pp.P99)
		}
		rows = append(rows, row)
	}
	return rows
}

// Combined mirrors report._combined: the weighted average of each per-category
// figure, over the rows that have it. Nil when nothing was measured.
type Combined struct {
	Decode, ITLLow1, UpdateP50, UpdateP99, TTFTP50 *float64
}

func combined(rows []CategoryRow, weights map[string]float64) *Combined {
	if len(rows) == 0 {
		return nil
	}
	w := map[string]float64{}
	tot := 0.0
	for _, r := range rows {
		tot += weights[r.Name]
	}
	if tot <= 0 {
		for _, r := range rows {
			w[r.Name] = 1
		}
	} else {
		w = weights
	}
	wavg := func(get func(CategoryRow) *float64) *float64 {
		sumW, sumWV := 0.0, 0.0
		for _, r := range rows {
			if v := get(r); v != nil {
				sumW += w[r.Name]
				sumWV += w[r.Name] * *v
			}
		}
		if sumW == 0 {
			return nil
		}
		return f(sumWV / sumW)
	}
	return &Combined{
		Decode:    wavg(func(r CategoryRow) *float64 { return r.DecodeMed }),
		ITLLow1:   wavg(func(r CategoryRow) *float64 { return r.ITLLow1 }),
		UpdateP50: wavg(func(r CategoryRow) *float64 { return r.UpdateP50 }),
		UpdateP99: wavg(func(r CategoryRow) *float64 { return r.UpdateP99 }),
		TTFTP50:   wavg(func(r CategoryRow) *float64 { return r.TTFTP50 }),
	}
}

// UnderSampled is one entry of report.sample_gate()["under_sampled"].
type UnderSampled struct {
	Section, Key, Metric string
	N, Need              int
}

// SampleGate mirrors report.sample_gate. It is recomputed from the samples
// rather than read from the file, so a schema-1 file (written before the gate
// was persisted) is judged by the same rule as a fresh one.
func (r *Results) SampleGate() []UnderSampled {
	var under []UnderSampled
	check := func(section, key, metric string, n int) {
		if !enoughSamplesForPercentile(n, 99) {
			under = append(under, UnderSampled{section, key, metric, n, samplesNeeded(99)})
		}
	}
	for _, row := range r.SingleRows() {
		check("single_stream", row.Name, "ttft_p99", row.TTFTN)
		if row.Batched {
			check("single_stream", row.Name, "update_p99", row.GapN)
		} else {
			check("single_stream", row.Name, "itl_high99", row.GapN)
		}
	}
	for _, c := range r.ConcurrencyRows() {
		check("concurrency", itoa(c.Level), "ttft_p99", c.TTFTN)
	}
	for _, d := range r.PrefillRows() {
		if !d.Skipped {
			check("prefill", itoa(d.TargetDepth), "pp_p99", d.PPN)
		}
	}
	return under
}
