package domain

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

const (
	assetCapPct  = 10.0 // layer 4: max weight per single asset (percent of portfolio)
	sectorCapPct = 40.0 // layer 4: max share of one sector among sector-bearing classes
	corrLimit    = 0.85
	maxPicks     = 6
)

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

type ProductAsset struct {
	Ticker string   `json:"ticker"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"` // asset | instrument (class has no catalog data)
	Weight float64  `json:"weight"`
	Score  *float64 `json:"score"`
	Sector string   `json:"sector,omitempty"`
	Held   bool     `json:"held"`
	Reason Text     `json:"reason"`
}

type ProductClass struct {
	Class       string         `json:"class"`
	Label       Text           `json:"label"`
	Allocation  float64        `json:"allocation"`
	CurrentPct  float64        `json:"currentPct"`
	Unallocated float64        `json:"unallocated"`
	Assets      []ProductAsset `json:"assets"`
}

type Flag struct {
	Code string `json:"code"`
	Text Text   `json:"text"`
}

type GateCount struct {
	Gate     string `json:"gate"`
	Excluded int    `json:"excluded"`
}

type ProductTrace struct {
	TargetAllocation AllocationTrace       `json:"targetAllocation"`
	GatesApplied     []GateCount           `json:"gatesApplied"`
	GateNotes        []string              `json:"gateNotes"`
	Exclusions       []Exclusion           `json:"exclusions"`
	ScoresPerAsset   map[string]ScoreTrace `json:"scoresPerAsset"`
	Diversification  []string              `json:"diversificationActions"`
}

type ProductOutput struct {
	Investable bool           `json:"investable"`
	Portfolio  []ProductClass `json:"portfolio"`
	Flags      []Flag         `json:"flags"`
	Trace      ProductTrace   `json:"trace"`
}

var instruments = map[string]struct {
	Name, Reason Text
}{
	ClassBondGov: {Text{"SBN ritel (ORI/SBR/SUN)", "Retail government bonds (ORI/SBR/SUN)"}, Text{"Diterbitkan pemerintah, likuiditas tinggi di pasar sekunder. Pilih seri yang sedang ditawarkan.", "Government issued with a liquid secondary market. Choose the series currently on offer."}},
	ClassSukuk:   {Text{"Sukuk negara ritel (SR/SWR)", "Retail government sukuk (SR/SWR)"}, Text{"Alternatif syariah untuk obligasi pemerintah. Pilih seri yang sedang ditawarkan.", "Sharia alternative to government bonds. Choose the series currently on offer."}},
	ClassFund:    {Text{"Reksa dana pasar uang", "Money-market mutual fund"}, Text{"Risiko rendah dan mudah dicairkan, cocok untuk dana yang dipakai dalam waktu dekat.", "Low risk and easy to redeem, suited to money needed soon."}},
	ClassDeposit: {Text{"Deposito berjangka (dijamin LPS)", "Time deposit (LPS insured)"}, Text{"Nilai pokok terjamin hingga batas penjaminan LPS, likuiditas tinggi.", "Principal insured up to the LPS limit, high liquidity."}},
}

func isInstrumentClass(c string) bool { _, ok := instruments[c]; return ok }

// Match runs the five layers. Pure: candidates, holdings and market facts are inputs.
func Match(p Profile, cands []Candidate, holdings []Holding, riskFree float64, macro MacroSignals) ProductOutput {
	out := ProductOutput{Portfolio: []ProductClass{}, Flags: []Flag{}}
	at := TargetAllocation(p)
	out.Investable = at.Investable
	out.Trace = ProductTrace{TargetAllocation: at, GateNotes: GateNotesFor(), Exclusions: []Exclusion{}, ScoresPerAsset: map[string]ScoreTrace{}, Diversification: []string{}, GatesApplied: []GateCount{}}
	if !at.Investable {
		out.Flags = append(out.Flags, Flag{"no_investment_allocation", Text{
			"Level tangga keuangan Anda masih 1-2: fokus dulu pada dana darurat dan utang, produk investasi belum direkomendasikan.",
			"Your financial ladder is still level 1-2: focus on the emergency fund and debt first; investment products are not recommended yet."}})
	}

	held := map[string]bool{}
	cur := map[string]float64{}
	total := 0.0
	for _, h := range holdings {
		if h.Symbol != "" {
			held[h.Symbol] = true
		}
		cur[h.Class] += h.ValueIDR
		total += h.ValueIDR
	}

	// layers 2 and 3
	counts := map[string]int{}
	byClass := map[string][]scored{}
	for _, c := range cands {
		if at.Target[c.Class] <= 0 {
			continue
		}
		if g, why := CheckGates(p, c); g != "" {
			counts[g]++
			out.Trace.Exclusions = append(out.Trace.Exclusions, Exclusion{c.Symbol, g, why})
			continue
		}
		s := Score(p, c, riskFree, macro)
		out.Trace.ScoresPerAsset[c.Symbol] = s
		byClass[c.Class] = append(byClass[c.Class], scored{c, s})
	}
	for _, g := range sortedKeys(counts) {
		out.Trace.GatesApplied = append(out.Trace.GatesApplied, GateCount{g, counts[g]})
	}

	// layer 4 and 5
	sectorTarget := at.Target[ClassLargeCap] + at.Target[ClassMidSmall] + at.Target[ClassETF]
	sectorW := map[string]float64{}
	var picked []scored
	for _, cl := range orderedClasses(at.Target) {
		alloc := at.Target[cl]
		pc := ProductClass{Class: cl, Label: ClassLabels[cl], Allocation: alloc, Assets: []ProductAsset{}}
		if total > 0 {
			pc.CurrentPct = round1(cur[cl] / total * 100)
		}
		if isInstrumentClass(cl) {
			in := instruments[cl]
			pc.Assets = append(pc.Assets, ProductAsset{Name: in.Name.ID + " / " + in.Name.EN, Kind: "instrument", Weight: alloc, Reason: in.Reason})
			out.Flags = append(out.Flags, Flag{"class_balance_" + cl, Text{
				"Data kelas " + ClassLabels[cl].ID + " belum ada di katalog, ditampilkan sebagai jenis instrumen tanpa skor.",
				"No catalog data for " + ClassLabels[cl].EN + ", shown as an instrument type without a score."}})
			out.Portfolio = append(out.Portfolio, pc)
			continue
		}
		list := byClass[cl]
		rank(list, held)
		n := min(maxPicks, int(math.Ceil(alloc/assetCapPct)))
		var chosen []scored
		for _, s := range list {
			if len(chosen) >= n {
				break
			}
			if o, r := mostCorrelated(s, picked); r > corrLimit {
				out.Trace.Diversification = append(out.Trace.Diversification, fmt.Sprintf("dropped %s: correlation %.2f with %s (> 0.85), kept the higher score", s.C.Symbol, r, o))
				continue
			}
			est := alloc / float64(n)
			if s.C.Sector != "" && sectorTarget > 0 && sectorW[s.C.Sector]+est > sectorCapPct/100*sectorTarget+1e-9 {
				out.Trace.Diversification = append(out.Trace.Diversification, fmt.Sprintf("skipped %s: sector %s would exceed 40%% of equity-like allocation", s.C.Symbol, s.C.Sector))
				continue
			}
			chosen = append(chosen, s)
			picked = append(picked, s)
			if s.C.Sector != "" {
				sectorW[s.C.Sector] += est
			}
		}
		weights, left := capWeights(alloc, chosen)
		for i, s := range chosen {
			if weights[i] <= 0 {
				continue
			}
			sc := s.S.Total
			pc.Assets = append(pc.Assets, ProductAsset{Ticker: s.C.Symbol, Name: s.C.Name, Kind: "asset", Weight: round1(weights[i]), Score: &sc, Sector: s.C.Sector, Held: held[s.C.Symbol], Reason: reason(s)})
		}
		if left > 0.05 {
			pc.Unallocated = round1(left)
			out.Trace.Diversification = append(out.Trace.Diversification, fmt.Sprintf("class %s: %.1f%% unallocated, %d eligible asset(s) cannot absorb it under the 10%% per-asset cap", cl, left, len(chosen)))
		}
		if len(pc.Assets) == 0 {
			out.Flags = append(out.Flags, Flag{"class_balance_" + cl, Text{
				"Tidak ada aset yang lolos untuk kelas " + ClassLabels[cl].ID + ".",
				"No asset passed the gates for " + ClassLabels[cl].EN + "."}})
		}
		out.Portfolio = append(out.Portfolio, pc)
	}
	return out
}

// capWeights spreads alloc over assets proportionally to score with a 10% cap; overflow goes to uncapped assets, the rest is returned unallocated.
func capWeights(alloc float64, in []scored) ([]float64, float64) {
	w := make([]float64, len(in))
	fixed := make([]bool, len(in))
	remaining := alloc
	for {
		sum := 0.0
		for i, s := range in {
			if !fixed[i] {
				sum += math.Max(s.S.Total, 1)
			}
		}
		if sum == 0 {
			return w, remaining
		}
		over := false
		for i, s := range in {
			if fixed[i] {
				continue
			}
			w[i] = remaining * math.Max(s.S.Total, 1) / sum
			if w[i] > assetCapPct {
				w[i], fixed[i], over = assetCapPct, true, true
			}
		}
		if !over {
			return w, 0
		}
		remaining = alloc
		for i := range in {
			if fixed[i] {
				remaining -= w[i]
			}
		}
		if remaining <= 1e-9 {
			for i := range in {
				if !fixed[i] {
					w[i] = 0
				}
			}
			return w, 0
		}
		allFixed := true
		for i := range in {
			allFixed = allFixed && fixed[i]
		}
		if allFixed {
			return w, remaining
		}
	}
}

func mostCorrelated(s scored, picked []scored) (string, float64) {
	best, who := -2.0, ""
	for _, o := range picked {
		if r, ok := correlation(s.C.Rets, o.C.Rets); ok && r > best {
			best, who = r, o.C.Symbol
		}
	}
	return who, best
}

// correlation is Pearson over the overlapping days; it needs at least 30 of them.
func correlation(a, b map[int]float64) (float64, bool) {
	var xs, ys []float64
	for d, x := range a {
		if y, ok := b[d]; ok {
			xs, ys = append(xs, x), append(ys, y)
		}
	}
	n := float64(len(xs))
	if n < 30 {
		return 0, false
	}
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/n, sy/n
	var cxy, vx, vy float64
	for i := range xs {
		cxy += (xs[i] - mx) * (ys[i] - my)
		vx += (xs[i] - mx) * (xs[i] - mx)
		vy += (ys[i] - my) * (ys[i] - my)
	}
	if vx == 0 || vy == 0 {
		return 0, false
	}
	return cxy / math.Sqrt(vx*vy), true
}

func reason(s scored) Text {
	c := s.C
	idp := []string{fmt.Sprintf("Skor %.0f", s.S.Total)}
	enp := []string{fmt.Sprintf("Score %.0f", s.S.Total)}
	idp = append(idp, fmt.Sprintf("return 1 tahun %s%%", fmtFloat(c.Return1Y)), "likuiditas "+map[string]string{"high": "tinggi", "medium": "sedang", "low": "rendah"}[c.Liquidity])
	enp = append(enp, fmt.Sprintf("1-year return %s%%", fmtFloat(c.Return1Y)), c.Liquidity+" liquidity")
	if c.Sector != "" {
		idp = append(idp, "sektor "+c.Sector)
		enp = append(enp, "sector "+c.Sector)
	}
	if c.DivYield != nil && *c.DivYield > 0 {
		idp = append(idp, fmt.Sprintf("dividen %s%%", fmtFloat(*c.DivYield)))
		enp = append(enp, fmt.Sprintf("dividend yield %s%%", fmtFloat(*c.DivYield)))
	}
	if c.Volatility == "high" {
		idp = append(idp, "volatilitas tinggi, harga bisa turun tajam")
		enp = append(enp, "high volatility, price can fall sharply")
	}
	return Text{join(idp), join(enp)}
}

func join(p []string) string {
	s := ""
	for i, x := range p {
		if i > 0 {
			s += "; "
		}
		s += x
	}
	return s + "."
}

// SortedWeights returns class allocations as integers that sum to 100 (largest remainder), for dashboard charts.
func SortedWeights(t map[string]float64) []struct {
	Class string
	Pct   int
} {
	type kv struct {
		k    string
		f    float64
		base int
	}
	var items []kv
	sum := 0
	for _, k := range orderedClasses(t) {
		b := int(math.Floor(t[k]))
		items = append(items, kv{k, t[k] - float64(b), b})
		sum += b
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return items[order[a]].f > items[order[b]].f })
	for i := 0; sum < 100 && len(order) > 0; i = (i + 1) % len(order) {
		items[order[i]].base++
		sum++
	}
	res := []struct {
		Class string
		Pct   int
	}{}
	for _, it := range items {
		if it.base > 0 {
			res = append(res, struct {
				Class string
				Pct   int
			}{it.k, it.base})
		}
	}
	return res
}
