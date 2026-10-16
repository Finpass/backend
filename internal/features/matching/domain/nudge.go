package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Nudge struct {
	Key    string `json:"key"`
	Action Text   `json:"action"`
	Reason Text   `json:"reason"`
	Amount *int64 `json:"amount,omitempty"`
}

// Rp formats an integer rupiah amount, e.g. Rp 1.000.000.
func Rp(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	r := "Rp " + strings.Join(out, ".")
	if neg {
		r = "-" + r
	}
	return r
}

func roundUp(v, step int64) int64 {
	if v <= 0 {
		return 0
	}
	return (v + step - 1) / step * step
}

func clampI(v, lo, hi int64) int64 { return max(lo, min(hi, v)) }

func ptr(v int64) *int64 { return &v }

func months(p Profile) float64 { return math.Round(p.EmergencyMonths*10) / 10 }

var goalNames = map[string]Text{
	"emergency_fund": {"dana darurat", "emergency fund"}, "home_purchase": {"membeli rumah", "buying a home"},
	"retirement": {"dana pensiun", "retirement"}, "children_education": {"pendidikan anak", "children's education"},
	"hajj_umrah": {"haji/umrah", "hajj/umrah"}, "marriage": {"pernikahan", "marriage"}, "wealth_growth": {"pertumbuhan kekayaan", "wealth growth"},
}

func horizonMonths(p Profile) float64 { return math.Max(6, p.HorizonYears()*12) }

// buildNudge returns the nudge for a category, or false when the category does not apply to this user.
func buildNudge(p Profile, key string) (Nudge, bool) {
	switch key {
	case "emergency_fund":
		amt := clampI(roundUp(int64(math.Max(0, float64(p.Surplus)))/4, 50_000), 100_000, 2_000_000)
		target := math.Round(p.EmergencyTargetMax*10) / 10
		now := fmt.Sprintf("%.1f", months(p))
		if p.EmergencyMonths < 1 {
			return Nudge{key, Text{"Mulai dari kecil: sisihkan " + Rp(amt) + " minggu ini untuk dana darurat.", "Start small: set aside " + Rp(amt) + " this week for your emergency fund."},
				Text{fmt.Sprintf("Dana darurat Anda kurang dari 1 bulan. Target: %.1f bulan.", target), fmt.Sprintf("Your emergency fund covers less than 1 month. Target: %.1f months.", target)}, ptr(amt)}, true
		}
		return Nudge{key, Text{"Sisihkan " + Rp(amt) + " minggu ini untuk dana darurat.", "Set aside " + Rp(amt) + " this week for your emergency fund."},
			Text{fmt.Sprintf("Dana darurat Anda %s bulan. Target: %.1f bulan.", now, target), fmt.Sprintf("Your emergency fund covers %s months. Target: %.1f months.", now, target)}, ptr(amt)}, true
	case "debt":
		if p.Installment <= 0 {
			return Nudge{}, false
		}
		amt := clampI(roundUp(int64(math.Max(0, float64(p.Surplus)))/4, 50_000), 100_000, 5_000_000)
		pct := 0.0
		if p.Income > 0 {
			pct = float64(p.Installment) / float64(p.Income) * 100
		}
		return Nudge{key, Text{"Bayar tambahan " + Rp(amt) + " ke utang berbunga tertinggi.", "Pay an extra " + Rp(amt) + " toward your highest-interest debt."},
			Text{fmt.Sprintf("Cicilan Anda %.1f%% dari penghasilan.", pct), fmt.Sprintf("Your installments are %.1f%% of income.", pct)}, ptr(amt)}, true
	case "learning":
		return Nudge{key, Text{"Luangkan 10 menit untuk belajar satu konsep keuangan dasar.", "Spend 10 minutes learning one basic financial concept."},
			Text{fmt.Sprintf("Skor pengetahuan keuangan Anda %.0f/100.", p.Health.Wisdom), fmt.Sprintf("Your financial wisdom score is %.0f/100.", p.Health.Wisdom)}, nil}, true
	case "investment":
		if p.Ladder < 3 || p.Income <= 0 {
			return Nudge{}, false
		}
		ratio := float64(p.Investable) / float64(p.Income) * 100
		room := p.Surplus - p.Investable
		add := roundUp(int64(0.20*float64(p.Income))-p.Investable, 50_000)
		if p.Investable == 0 {
			add = clampI(roundUp(int64(0.10*float64(p.Income)), 50_000), 100_000, 5_000_000)
			if p.Surplus > 0 && add > p.Surplus {
				add = p.Surplus / 50_000 * 50_000
			}
			if add <= 0 {
				return Nudge{}, false
			}
			return Nudge{key, Text{"Mulai investasi " + Rp(add) + "/bulan di dana yang terdiversifikasi.", "Start investing " + Rp(add) + "/month in a diversified fund."},
				Text{"Saat ini belum ada dana yang Anda alokasikan untuk investasi bulanan.", "You have not set aside any monthly amount for investing yet."}, ptr(add)}, true
		}
		if ratio >= 20 || add <= 0 || room <= 0 {
			return Nudge{}, false
		}
		if add > room {
			add = room / 50_000 * 50_000
			if add <= 0 {
				return Nudge{}, false
			}
		}
		return Nudge{key, Text{"Tingkatkan investasi bulanan.", "Increase your monthly investment."},
			Text{fmt.Sprintf("Anda investasi %.1f%% dari income. Target: 20%%. Tambah %s/bulan.", ratio, Rp(add)), fmt.Sprintf("You invest %.1f%% of income. Target: 20%%. Add %s/month.", ratio, Rp(add))}, ptr(add)}, true
	case "goal":
		g, ok := goalNames[p.Goal]
		if !ok {
			g = Text{"tujuan keuangan", "financial goal"}
		}
		if p.GoalAmount > 0 {
			need := roundUp(int64(float64(p.GoalAmount)/horizonMonths(p)), 50_000)
			return Nudge{key, Text{"Tinjau rencana " + g.ID + " Anda.", "Review your " + g.EN + " plan."},
				Text{"Perkiraan setoran bulanan agar tercapai: " + Rp(need) + ".", "Estimated monthly contribution to get there: " + Rp(need) + "."}, ptr(need)}, true
		}
		return Nudge{key, Text{"Tinjau rencana " + g.ID + " Anda.", "Review your " + g.EN + " plan."},
			Text{fmt.Sprintf("Skor kebebasan finansial Anda %.0f/100.", p.Health.Freedom), fmt.Sprintf("Your financial freedom score is %.0f/100.", p.Health.Freedom)}, nil}, true
	case "raise_contribution":
		if p.Income <= 0 {
			return Nudge{}, false
		}
		g, ok := goalNames[p.Goal]
		if !ok {
			g = Text{"tujuan Anda", "your goal"}
		}
		amt := clampI(roundUp(int64(0.05*float64(p.Income)), 50_000), 100_000, 10_000_000)
		return Nudge{key, Text{"Naikkan setoran bulanan " + Rp(amt) + " untuk " + g.ID + ".", "Increase your monthly contribution by " + Rp(amt) + " for " + g.EN + "."},
			Text{"Menambah 5% dari penghasilan mempercepat tujuan Anda.", "Adding 5% of income brings your goal closer."}, ptr(amt)}, true
	case "estate":
		return Nudge{key, Text{"Tinjau perencanaan warisan bersama profesional.", "Review estate planning with a professional."},
			Text{"Aset yang sudah mapan perlu rencana pengalihan yang jelas.", "Established wealth benefits from a clear transfer plan."}, nil}, true
	case "review":
		return Nudge{key, Text{"Tinjau ringkasan keuangan Anda di dashboard.", "Review your financial summary on the dashboard."},
			Text{"Pantau perkembangan secara berkala.", "Keep an eye on your progress regularly."}, nil}, true
	}
	return Nudge{}, false
}

// nudgeCategories orders candidate categories: health scores below 40 first (safety, wisdom, growth, freedom), then the ladder default.
func nudgeCategories(p Profile) []string {
	var c []string
	hasDebt := false
	for _, x := range p.Priorities {
		hasDebt = hasDebt || x == "debt_management"
	}
	if p.Health.Safety < 40 {
		c = append(c, "emergency_fund")
	}
	if p.Ladder == 2 || (p.Ladder <= 2 && hasDebt) {
		c = append(c, "debt")
	}
	if p.Health.Wisdom < 40 {
		c = append(c, "learning")
	}
	if p.Health.Growth < 40 {
		c = append(c, "investment")
	}
	if p.Health.Freedom < 40 {
		c = append(c, "goal")
	}
	switch p.Ladder {
	case 1:
		c = append(c, "emergency_fund")
	case 2:
		c = append(c, "debt", "emergency_fund")
	case 3:
		c = append(c, "investment", "goal")
	case 4:
		c = append(c, "raise_contribution", "goal")
	default:
		c = append(c, "estate", "raise_contribution")
	}
	c = append(c, "review")
	return c
}

// MatchNudge implements daily layer 4. A nudge that was sent the two previous days is skipped (3 days in a row rule).
func MatchNudge(p Profile, hist []DayRecord) (Nudge, []string) {
	var trace []string
	var first *Nudge
	seen := map[string]bool{}
	for _, k := range nudgeCategories(p) {
		if seen[k] {
			continue
		}
		seen[k] = true
		n, ok := buildNudge(p, k)
		if !ok {
			trace = append(trace, k+": not applicable")
			continue
		}
		if first == nil {
			c := n
			first = &c
		}
		if len(hist) >= 2 && hist[0].NudgeKey == k && hist[1].NudgeKey == k {
			trace = append(trace, k+": skipped, same nudge 2 days in a row")
			continue
		}
		trace = append(trace, k+": chosen")
		return n, trace
	}
	return *first, append(trace, first.Key+": reused, no alternative")
}
