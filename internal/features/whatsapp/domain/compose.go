package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits from the WA Delivery YAML (characters).
const (
	MaxTotal  = 800
	MaxSlot0  = 180
	MaxSlot1  = 200
	MaxSlot2  = 150
	MaxSlot3  = 180
	MaxFooter = 90
)

const (
	SlotAlert = "slot_0_alert"
	SlotNews  = "slot_1_news"
	SlotMacro = "slot_2_macro"
	SlotNudge = "slot_3_nudge"
)

type Text struct{ ID, EN string }

func (t Text) In(l string) string {
	if l == "en" {
		return t.EN
	}
	return t.ID
}

// Content is the matching output reduced to what the templates need (numbers already decided by the engines).
type Content struct {
	Name        string
	Risk        string
	Ladder      int
	HealthTotal float64
	HealthCat   string
	Alert       *Alert
	News        *News
	MoreNews    []News
	Macro       *Macro
	Nudge       Nudge
	Quiet       bool // no alert for 5 days: market summary instead
	Degraded    bool // data fetch failure
	NewUser     bool
}

type Alert struct {
	Type, Asset, Direction, Detail string
	ChangePct                      float64
	PositionIDR                    int64
	Amount                         float64
	Repeated                       bool
}

type News struct {
	Title, Summary, URL string
	Reason              Text
	NumbersSafe         bool
	Followup            bool
}

type Macro struct {
	Label     Text
	ValueText string
	Change    *float64
	Headline  string
	Reason    Text
}

type Nudge struct {
	Key            string
	Action, Reason Text
}

type Slot struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type Briefing struct {
	Text      string   `json:"text"`
	Slots     []Slot   `json:"slots"`
	CharCount int      `json:"charCount"`
	Fallback  string   `json:"fallback,omitempty"`
	Dropped   []string `json:"dropped,omitempty"`
}

// Options are the rendering inputs that are not content.
type Options struct {
	Locale string
	Link   string
	Now    time.Time // user-local
}

var monthsID = []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
var monthsEN = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func dateText(t time.Time, l string) string {
	m := monthsID
	if l == "en" {
		m = monthsEN
	}
	return fmt.Sprintf("%d %s", t.Day(), m[t.Month()-1])
}

func pct(v float64) string {
	return strconv.FormatFloat(math.Round(math.Abs(v)*100)/100, 'f', -1, 64)
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// clip shortens s to n runes at a word boundary with "...".
func clip(s string, n int) string {
	if runes(s) <= n {
		return s
	}
	r := []rune(s)[:n-3]
	t := string(r)
	if i := strings.LastIndex(t, " "); i > n/2 {
		t = t[:i]
	}
	return strings.TrimRight(t, " ,.;:") + "..."
}

// fit joins parts with newlines; when over max it removes optional parts from the end first, then clips the first part.
func fit(limit int, parts []string, optional int) string {
	for {
		s := strings.Join(parts, "\n")
		if runes(s) <= limit {
			return s
		}
		if optional > 0 && len(parts) > 1 {
			parts = parts[:len(parts)-1]
			optional--
			continue
		}
		if len(parts) == 1 {
			return clip(parts[0], limit)
		}
		over := runes(s) - limit
		parts[len(parts)-1] = clip(parts[len(parts)-1], max(8, runes(parts[len(parts)-1])-over))
		s = strings.Join(parts, "\n")
		if runes(s) > limit {
			return clip(s, limit)
		}
		return s
	}
}

func assetName(sym string) string {
	if i := strings.Index(sym, "."); i > 0 {
		return strings.ToUpper(sym[:i])
	}
	return strings.ToUpper(sym)
}

func word(dir, l string) string {
	if l == "en" {
		if dir == "down" {
			return "fell"
		}
		return "rose"
	}
	if dir == "down" {
		return "turun"
	}
	return "naik"
}

func alertSlot(a Alert, risk, l string) string {
	n := assetName(a.Asset)
	var parts []string
	switch a.Type {
	case "corporate_action":
		if l == "en" {
			parts = []string{fmt.Sprintf("Corporate action: %s dividend %s per share, ex-date reached.", n, strconv.FormatFloat(a.Amount, 'f', -1, 64))}
		} else {
			parts = []string{fmt.Sprintf("Aksi korporasi: %s dividen %s per saham, tanggal ex tercapai.", n, strconv.FormatFloat(a.Amount, 'f', -1, 64))}
		}
		return fit(MaxSlot0, parts, 0)
	case "52w_high_low":
		hi := a.Detail == "high"
		switch {
		case l == "en" && hi:
			parts = []string{fmt.Sprintf("%s reached a new 52-week high.", n)}
		case l == "en":
			parts = []string{fmt.Sprintf("%s hit a new 52-week low.", n)}
		case hi:
			parts = []string{fmt.Sprintf("%s mencapai harga tertinggi 52 minggu.", n)}
		default:
			parts = []string{fmt.Sprintf("%s menyentuh harga terendah 52 minggu.", n)}
		}
	default:
		switch {
		case a.Repeated && l == "en":
			parts = []string{fmt.Sprintf("%s is still moving: %s %s%% yesterday, third day in a row.", n, word(a.Direction, l), pct(a.ChangePct))}
		case a.Repeated:
			parts = []string{fmt.Sprintf("%s masih bergerak: %s %s%% kemarin, hari ketiga berturut-turut.", n, word(a.Direction, l), pct(a.ChangePct))}
		case l == "en":
			parts = []string{fmt.Sprintf("%s %s %s%% yesterday.", n, word(a.Direction, l), pct(a.ChangePct))}
		default:
			parts = []string{fmt.Sprintf("%s %s %s%% kemarin.", n, word(a.Direction, l), pct(a.ChangePct))}
		}
	}
	opt := 0
	if a.PositionIDR != 0 {
		amt := a.PositionIDR
		if amt < 0 {
			amt = -amt
		}
		if l == "en" {
			parts = append(parts, fmt.Sprintf("Your position %s ~%s.", word(a.Direction, l), rp(amt)))
		} else {
			parts = append(parts, fmt.Sprintf("Posisi Anda %s ~%s.", word(a.Direction, l), rp(amt)))
		}
		opt++
	}
	if a.Direction == "down" && risk != "Aggressive" && math.Abs(a.ChangePct) < 10 { // no "nothing to do" after a very large fall
		if l == "en" {
			parts = append(parts, "No action is needed right now.")
		} else {
			parts = append(parts, "Tidak ada aksi yang diperlukan saat ini.")
		}
		opt++
	}
	return fit(MaxSlot0, parts, opt)
}

func rp(n int64) string {
	s := strconv.FormatInt(n, 10)
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	return "Rp " + strings.Join(out, ".")
}

func newsSlot(n News, l string) string {
	pre, rel, upd := "Berita: ", "Relevan karena %s", "Update terbaru."
	if l == "en" {
		pre, rel, upd = "News: ", "Relevant because %s", "Latest update."
	}
	reason := strings.TrimRight(n.Reason.In(l), ".")
	parts := []string{pre + clip(n.Title, 90)}
	if n.Summary != "" && n.NumbersSafe {
		parts = append(parts, n.Summary)
	}
	if reason != "" {
		parts = append(parts, fmt.Sprintf(rel, lowerFirst(reason))+".")
	}
	if n.Followup {
		parts = append(parts, upd)
	}
	return fit(MaxSlot1, parts, 1+btoi(n.Followup))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) < 2 || r[1] < 'a' || r[1] > 'z' || strings.HasPrefix(s, "Anda") {
		return s // acronyms, proper names and "Anda" keep their capital
	}
	r[0] = []rune(strings.ToLower(string(r[0])))[0]
	return string(r)
}

func macroSlot(m Macro, l string) string {
	pre := "Makro: "
	if l == "en" {
		pre = "Macro: "
	}
	var head string
	switch {
	case m.ValueText != "" && m.Change != nil:
		w := map[bool]string{true: "naik", false: "turun"}
		if l == "en" {
			w = map[bool]string{true: "up", false: "down"}
		}
		head = fmt.Sprintf("%s%s %s (%s %s%%).", pre, m.Label.In(l), m.ValueText, w[*m.Change >= 0], pct(*m.Change))
	case m.ValueText != "":
		head = fmt.Sprintf("%s%s %s.", pre, m.Label.In(l), m.ValueText)
	default:
		head = pre + m.Label.In(l) + ": " + clip(m.Headline, 90)
	}
	return fit(MaxSlot2, []string{head, m.Reason.In(l)}, 1)
}

func nudgeSlot(n Nudge, l string) string {
	pre := "Satu langkah: "
	if l == "en" {
		pre = "One step: "
	}
	return fit(MaxSlot3, []string{pre + n.Action.In(l), n.Reason.In(l)}, 0)
}

func greeting(name, l string, date time.Time, newUser bool) string {
	d := dateText(date, l)
	switch {
	case newUser && l == "en":
		return fmt.Sprintf("Welcome, %s! This is your first briefing for %s.", name, d)
	case newUser:
		return fmt.Sprintf("Selamat datang, %s! Ini briefing pertama Anda untuk %s.", name, d)
	case l == "en":
		return fmt.Sprintf("Good morning, %s. Here is your financial briefing for %s.", name, d)
	}
	return fmt.Sprintf("Selamat pagi, %s. Berikut briefing keuangan Anda untuk %s.", name, d)
}

func footer(link, l string) string {
	full := "Detail di dashboard: " + link + "\nBalas MORE untuk berita lain, STOP untuk berhenti."
	short := "Detail: " + link + "\nBalas MORE atau STOP."
	if l == "en" {
		full = "Details on the dashboard: " + link + "\nReply MORE for more news, STOP to stop."
		short = "Details: " + link + "\nReply MORE or STOP."
	}
	if runes(full) <= MaxFooter {
		return full
	}
	return short
}

// Compose renders the daily briefing. Slot 3 (nudge) is mandatory; when the total exceeds 800 characters the lowest priority
// slots are dropped (macro, then news, then alert).
func Compose(c Content, o Options) Briefing {
	l := o.Locale
	if l != "en" {
		l = "id"
	}
	b := Briefing{}
	add := func(key, text string) { b.Slots = append(b.Slots, Slot{key, text}) }

	nudge := nudgeSlot(c.Nudge, l)
	foot := footer(o.Link, l)
	hello := greeting(c.Name, l, o.Now, c.NewUser)

	if c.Degraded { // data fetch failure: nudge plus an apology
		b.Fallback = "data_fetch_failure"
		add(SlotNudge, nudge)
		sorry, later := "Kami sedang mengalami gangguan data hari ini.", "Coba cek dashboard nanti: "
		if l == "en" {
			sorry, later = "We are having data issues today.", "Please check the dashboard later: "
		}
		hi := strings.SplitN(hello, ".", 2)[0] + "."
		if c.NewUser {
			hi = hello
		}
		return finish(b, strings.Join([]string{hi, sorry, nudge, later + o.Link}, "\n"))
	}

	var alert, news, macro string
	switch {
	case c.Alert != nil:
		alert = alertSlot(*c.Alert, c.Risk, l)
	case c.Quiet:
		alert = map[string]string{"id": "Ringkasan pasar: tidak ada pergerakan besar pada aset Anda dalam 5 hari terakhir.", "en": "Market summary: no large moves in your assets in the last 5 days."}[l]
	}
	if c.News != nil {
		news = newsSlot(*c.News, l)
	}
	if c.Macro != nil {
		macro = macroSlot(*c.Macro, l)
	}
	if alert == "" && news == "" && macro == "" { // nothing but the nudge
		b.Fallback = "no_content_available"
		add(SlotNudge, nudge)
		calm := map[string]string{"id": "Pasar tenang hari ini. Tidak ada berita penting.", "en": "The market is calm today. No important news."}[l]
		hi := strings.SplitN(hello, ".", 2)[0] + "."
		if c.NewUser {
			hi = hello
		}
		return finish(b, strings.Join([]string{hi, calm, nudge, foot}, "\n"))
	}

	type slot struct{ key, text string }
	slots := []slot{{SlotAlert, alert}, {SlotNews, news}, {SlotMacro, macro}, {SlotNudge, nudge}}
	render := func() string {
		parts := []string{hello}
		for _, s := range slots {
			if s.text != "" {
				parts = append(parts, s.text)
			}
		}
		return strings.Join(append(parts, foot), "\n\n")
	}
	for runes(render()) > MaxTotal {
		dropped := false
		for i := len(slots) - 2; i >= 0; i-- { // lowest priority first, never the nudge
			if slots[i].text != "" {
				b.Dropped = append(b.Dropped, slots[i].key)
				slots[i].text = ""
				dropped = true
				break
			}
		}
		if !dropped {
			break
		}
	}
	for _, s := range slots {
		if s.text != "" {
			add(s.key, s.text)
		}
	}
	return finish(b, render())
}

func finish(b Briefing, text string) Briefing {
	b.Text, b.CharCount = text, runes(text)
	return b
}

// ComposeReengage is the message for users who have not engaged for a while.
func ComposeReengage(name string, days int, l string) string {
	if l == "en" {
		return fmt.Sprintf("Hello, %s. We miss you!\nYou have not engaged with the briefing for %d days.\nReply RESUME to continue, or STOP to stop.", name, days)
	}
	return fmt.Sprintf("Halo, %s. Kami rindu!\nAnda belum membuka briefing selama %d hari.\nBalas RESUME untuk lanjut, atau STOP untuk berhenti.", name, days)
}

// ReplyContext carries what command responses need.
type ReplyContext struct {
	Locale  string
	Name    string
	Link    string
	Content Content
}

// Reply renders the response to a command. It does not change state; the application layer does.
func Reply(cmd Command, rc ReplyContext) string {
	l := rc.Locale
	if l != "en" {
		l = "id"
	}
	help := map[string]string{
		"id": "Command yang tersedia:\n- MORE: berita tambahan\n- STOP: pause briefing\n- RESUME: lanjut briefing\n- PROFILE: lihat profil\n- UPDATE: lengkapi profil\n- HELP: bantuan",
		"en": "Available commands:\n- MORE: more news\n- STOP: pause the briefing\n- RESUME: continue the briefing\n- PROFILE: see your profile\n- UPDATE: complete your profile\n- HELP: help",
	}[l]
	switch cmd {
	case CmdMore:
		items := []News{}
		if rc.Content.News != nil {
			items = append(items, *rc.Content.News)
		}
		items = append(items, rc.Content.MoreNews...)
		if len(items) > 2 {
			items = items[:2]
		}
		if len(items) == 0 {
			return map[string]string{"id": "Belum ada berita lain yang relevan untuk Anda hari ini.\nDetail di dashboard: " + rc.Link, "en": "No other relevant news for you today.\nDetails on the dashboard: " + rc.Link}[l]
		}
		head := map[string]string{"id": "Berita lain untuk Anda:", "en": "More news for you:"}[l]
		lines := []string{head}
		for _, n := range items {
			lines = append(lines, "- "+clip(n.Title, 110))
		}
		lines = append(lines, map[string]string{"id": "Detail di dashboard: ", "en": "Details on the dashboard: "}[l]+rc.Link)
		return strings.Join(lines, "\n")
	case CmdStop:
		return map[string]string{"id": "Baik, briefing Anda dijeda.\nBalas RESUME kapan saja untuk lanjut.", "en": "Okay, your briefing is paused.\nReply RESUME any time to continue."}[l]
	case CmdResume:
		return map[string]string{"id": "Briefing Anda aktif kembali!\nSampai jumpa besok pagi.", "en": "Your briefing is active again!\nSee you tomorrow morning."}[l]
	case CmdProfile:
		c := rc.Content
		if l == "en" {
			return fmt.Sprintf("Your profile:\n- Risk: %s\n- Level: %d\n- Financial Health: %.0f/100 (%s)\nDetails on the dashboard: %s", c.Risk, c.Ladder, c.HealthTotal, c.HealthCat, rc.Link)
		}
		return fmt.Sprintf("Profil Anda:\n- Risiko: %s\n- Level: %d\n- Financial Health: %.0f/100 (%s)\nDetail di dashboard: %s", c.Risk, c.Ladder, c.HealthTotal, c.HealthCat, rc.Link)
	case CmdUpdate:
		return map[string]string{
			"id": "Ayo perbarui profil Anda!\nJawaban bisa diperbarui kapan saja di dashboard.\nMulai di sini: " + rc.Link,
			"en": "Let's update your profile!\nYou can change your answers on the dashboard at any time.\nStart here: " + rc.Link}[l]
	case CmdQuestion:
		return map[string]string{"id": "Fitur tanya jawab belum tersedia di WhatsApp.\n" + help, "en": "Questions are not available on WhatsApp yet.\n" + help}[l]
	}
	return help
}
