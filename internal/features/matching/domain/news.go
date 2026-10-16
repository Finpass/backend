package domain

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

const newsThreshold = 70.0

type NewsPick struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Reason    Text      `json:"relevanceReason"`
	Source    string    `json:"source"`
	URL       string    `json:"url"`
	Category  string    `json:"category"`
	Topic     string    `json:"topic"`
	Score     float64   `json:"score"`
	Published time.Time `json:"publishedAt"`
	// NumbersSafe is false for tier 3 market updates: the briefing must not quote numbers from them.
	NumbersSafe bool `json:"numbersSafe"`
	Followup    bool `json:"followup"` // same topic 3 days in a row: close with "latest update"
}

type NewsTrace struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Score    float64            `json:"score"`
	Parts    map[string]float64 `json:"parts"`
	Decision string             `json:"decision"`
}

var (
	tier1 = []string{"bank indonesia", "bps", "ojk", "idx", "kemenkeu", "kementerian keuangan"}
	tier2 = []string{"cnbc", "cnn indonesia", "kontan", "bisnis", "reuters", "bloomberg"}

	highVolRe = regexp.MustCompile(`(?i)anjlok|ambles|melonjak|meroket|crash|jatuh bebas|kripto|crypto|bitcoin|saham gorengan|volatil|turun tajam`)
	macroHiRe = regexp.MustCompile(`(?i)bi rate|suku bunga|inflasi|inflation|ojk|regulasi|aturan|peraturan`)
	digitRe   = regexp.MustCompile(`\d`)
)

var classKeywords = map[string]*regexp.Regexp{
	ClassBondGov: regexp.MustCompile(`(?i)bi rate|suku bunga|obligasi|sbn|\bsun\b|yield`),
	ClassSukuk:   regexp.MustCompile(`(?i)sukuk|suku bunga|bi rate`),
	ClassFund:    regexp.MustCompile(`(?i)reksa dana|reksadana|bi rate|suku bunga|obligasi`),
	ClassDeposit: regexp.MustCompile(`(?i)deposito|suku bunga|bi rate|lps`),
	ClassGold:    regexp.MustCompile(`(?i)emas|gold|antam`),
	ClassCrypto:  regexp.MustCompile(`(?i)kripto|crypto|bitcoin|ethereum`),
}

var goalKeywords = map[string]*regexp.Regexp{
	"emergency_fund":     regexp.MustCompile(`(?i)dana darurat|tabungan|deposito|suku bunga|inflasi|phk`),
	"home_purchase":      regexp.MustCompile(`(?i)kpr|properti|rumah|suku bunga`),
	"retirement":         regexp.MustCompile(`(?i)pensiun|dplk|bpjs|obligasi|dividen`),
	"children_education": regexp.MustCompile(`(?i)pendidikan|beasiswa|sekolah|kuliah`),
	"hajj_umrah":         regexp.MustCompile(`(?i)haji|umrah|emas|sukuk|kurs`),
	"marriage":           regexp.MustCompile(`(?i)nikah|pernikahan|emas|tabungan`),
	"wealth_growth":      regexp.MustCompile(`(?i)saham|ihsg|etf|reksa dana|investasi|dividen`),
}

func tierOf(source string) int {
	s := strings.ToLower(source)
	for _, t := range tier1 {
		if strings.Contains(s, t) {
			return 1
		}
	}
	for _, t := range tier2 {
		if strings.Contains(s, t) {
			return 2
		}
	}
	return 3
}

func baseSymbol(s string) string {
	s = strings.ToUpper(s)
	if i := strings.Index(s, "."); i > 0 {
		s = s[:i]
	}
	return s
}

// MatchNews implements daily layer 2. It returns the pick (nil below threshold), the next best items for MORE and a trace.
func MatchNews(p Profile, holdings []Holding, arts []Article, hist []DayRecord, now time.Time) (*NewsPick, []NewsPick, []NewsTrace) {
	held := map[string]string{}
	classes := map[string]bool{}
	for _, h := range holdings {
		if h.Symbol != "" {
			held[baseSymbol(h.Symbol)] = h.Name
		}
		classes[h.Class] = true
	}
	sentRecent := map[string]bool{}
	for _, d := range hist {
		sentRecent[d.NewsID] = true
	}
	type cand struct {
		pick  NewsPick
		trace NewsTrace
		ok    bool
	}
	var cands []cand
	for _, a := range arts {
		text := a.Title + " " + a.Summary
		parts := map[string]float64{}
		reason := Text{"Berita pasar umum.", "General market news."}

		hm := 0.0
		for sym, name := range held { // IDX news carries no ticker tags: match the symbol or company name in the text
			if mentions(text, sym, name) {
				hm = 100
				reason = Text{"Anda punya " + shortName(name, sym), "You hold " + shortName(name, sym)}
				break
			}
		}
		for _, t := range a.Tickers {
			if hm != 0 {
				break
			}
			if name, ok := held[baseSymbol(t)]; ok {
				hm = 100
				reason = Text{"Anda punya " + shortName(name, t), "You hold " + shortName(name, t)}
				break
			}
		}
		if hm == 0 {
			for c, re := range classKeywords {
				if classes[c] && re.MatchString(text) {
					hm = 80
					reason = Text{"Anda punya " + strings.ToLower(ClassLabels[c].ID), "You hold " + strings.ToLower(ClassLabels[c].EN)}
					break
				}
			}
		}
		if hm == 0 && len(a.Tickers) > 0 {
			hm = 30
		}
		gm := 0.0
		if re, ok := goalKeywords[p.Goal]; ok && re.MatchString(text) {
			gm = 100
			if hm < 100 {
				reason = Text{"Terkait tujuan Anda: " + goalNames[p.Goal].ID, "Related to your goal: " + goalNames[p.Goal].EN}
			}
		} else if a.Category == "capital_markets" || a.Category == "macro" {
			gm = 40
		}
		mr := 20.0
		switch {
		case macroHiRe.MatchString(a.Title):
			mr = 80
			if hm == 0 && gm < 100 {
				reason = Text{"Memengaruhi suku bunga, inflasi, atau regulasi.", "Affects interest rates, inflation or regulation."}
			}
		case a.Category == "macro":
			mr = 60
		case a.Category == "capital_markets":
			mr = 40
		}
		pa := 60.0
		decision := ""
		if p.Risk == Conservative && highVolRe.MatchString(a.Title) {
			pa, decision = 0, "filtered: high-volatility news for a Conservative profile"
		} else if p.Risk == Conservative && a.Sentiment != nil && *a.Sentiment < 0 {
			pa, decision = 0, "held for review: negative sentiment for a Conservative profile"
		} else if hm >= 80 || (p.Risk != Conservative && gm == 100) {
			pa = 100
		}
		parts["holding_match"], parts["goal_match"], parts["macro_relevance"], parts["profile_alignment"] = hm, gm, mr, pa
		score := round1(hm*0.40 + gm*0.25 + mr*0.20 + pa*0.15)
		topic := a.Category
		if len(a.Tickers) > 0 {
			topic = baseSymbol(a.Tickers[0])
		}
		tier := tierOf(a.Source)
		pick := NewsPick{ID: a.ID, Title: a.Title, Summary: oneSentence(a.Summary), Reason: reason, Source: a.Source, URL: a.URL, Category: a.Category, Topic: topic, Score: score, Published: a.PublishedAt,
			NumbersSafe: !(tier == 3 && a.Category == "capital_markets") && !(tier == 3 && digitRe.MatchString(a.Title))}
		ok := decision == ""
		if ok && sentRecent[a.ID] {
			ok, decision = false, "skipped: already sent recently"
		}
		if decision == "" {
			decision = "candidate"
		}
		cands = append(cands, cand{pick, NewsTrace{ID: a.ID, Title: a.Title, Score: score, Parts: parts, Decision: decision}, ok})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].pick.Score != cands[j].pick.Score {
			return cands[i].pick.Score > cands[j].pick.Score
		}
		return cands[i].pick.Published.After(cands[j].pick.Published)
	})
	var chosen *NewsPick
	var more []NewsPick
	var trace []NewsTrace
	repeated := func(topic string) bool {
		return len(hist) >= 2 && hist[0].NewsTopic == topic && hist[1].NewsTopic == topic
	}
	for i := range cands {
		c := &cands[i]
		switch {
		case !c.ok:
		case chosen == nil && c.pick.Score >= newsThreshold && !repeated(c.pick.Topic):
			p := c.pick
			chosen = &p
			c.trace.Decision = "chosen"
		case chosen == nil && c.pick.Score >= newsThreshold:
			c.trace.Decision = "deferred: same topic 3 days in a row"
		case len(more) < 2 && c.pick.Score >= 50:
			more = append(more, c.pick)
			c.trace.Decision = "more"
		}
		trace = append(trace, c.trace)
	}
	if chosen == nil { // everything above threshold shared the repeated topic: keep the best and close with "latest update"
		for i := range cands {
			if cands[i].ok && cands[i].pick.Score >= newsThreshold {
				p := cands[i].pick
				p.Followup = true
				chosen = &p
				for j := range trace {
					if trace[j].ID == p.ID {
						trace[j].Decision = "chosen (follow-up on a repeated topic)"
					}
				}
				break
			}
		}
	}
	if len(trace) > 25 {
		trace = trace[:25]
	}
	return chosen, more, trace
}

var companyPrefix = regexp.MustCompile(`(?i)^pt\.?\s+|\s+tbk\.?$|\s*\(persero\)\s*`)

// mentions is true when text names the ticker as a whole upper-case word or contains the company name (without PT/Tbk).
func mentions(text, sym, name string) bool {
	if len(sym) >= 3 && regexp.MustCompile(`\b`+regexp.QuoteMeta(sym)+`\b`).MatchString(text) {
		return true
	}
	n := strings.TrimSpace(companyPrefix.ReplaceAllString(name, " "))
	return len(n) >= 8 && strings.Contains(strings.ToLower(text), strings.ToLower(n))
}

func shortName(name, ticker string) string {
	if n := strings.TrimSpace(companyPrefix.ReplaceAllString(name, " ")); n != "" {
		return n
	}
	return baseSymbol(ticker)
}

func oneSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return s
}
