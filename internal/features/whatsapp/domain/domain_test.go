package domain

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct{ in, want string }{
		{"081234567890", "+6281234567890"},
		{"0812-3456-7890", "+6281234567890"},
		{"+62 812 3456 7890", "+6281234567890"},
		{"6281234567890", "+6281234567890"},
		{"81234567890", "+6281234567890"},
		{"+620812345678", "+62812345678"},
		{"(0812) 345 678 90", "+6281234567890"},
		{"0062812345678", "+62812345678"},
		{"+14155550123", "+14155550123"},
		{"", ""}, {"abc", ""}, {"0812", ""}, {"+62 21 5551234", ""}, {"12345", ""}, {"+0123456789", ""}, {"0812345678901234567", ""},
	}
	for _, tc := range tests {
		got, err := NormalizePhone(tc.in)
		if tc.want == "" {
			if err == nil {
				t.Errorf("%q should fail, got %q", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q -> %q (%v), want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestMaskJID(t *testing.T) {
	if got := MaskPhone("+6281234567890"); got != "+62********890" || strings.Contains(got, "1234") {
		t.Errorf("mask %q", got)
	}
	if MaskPhone("123") != "***" {
		t.Error("short")
	}
	if JID("+6281234567890") != "6281234567890@s.whatsapp.net" {
		t.Error("jid")
	}
	for in, want := range map[string]string{"6281234567890@s.whatsapp.net": "+6281234567890", "6281234567890:12@s.whatsapp.net": "+6281234567890"} {
		if p, ok := PhoneFromJID(in); !ok || p != want {
			t.Errorf("%s -> %s", in, p)
		}
	}
	for _, in := range []string{"123@g.us", "251556368777322@lid", "", "abc@s.whatsapp.net"} {
		if _, ok := PhoneFromJID(in); ok {
			t.Errorf("%q should not parse", in)
		}
	}
}

func TestSignature(t *testing.T) {
	body := []byte(`{"event":"message"}`)
	good := Sign("s3cret", body)
	tests := []struct {
		name, secret, header string
		body                 []byte
		ok                   bool
	}{
		{"valid", "s3cret", good, body, true},
		{"wrong secret", "other", good, body, false},
		{"tampered body", "s3cret", good, []byte(`{"event":"messagE"}`), false},
		{"empty secret", "", good, body, false},
		{"missing prefix", "s3cret", strings.TrimPrefix(good, "sha256="), body, false},
		{"not hex", "s3cret", "sha256=zz", body, false},
		{"empty header", "s3cret", "", body, false},
	}
	for _, tc := range tests {
		if got := VerifySignature(tc.secret, tc.body, tc.header); got != tc.ok {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestParseCommand(t *testing.T) {
	tests := map[string]Command{
		"MORE": CmdMore, " more ": CmdMore, "More!": CmdMore, "stop": CmdStop, "STOP.": CmdStop, "berhenti": CmdStop, "Resume": CmdResume,
		"profile": CmdProfile, "UPDATE": CmdUpdate, "help": CmdHelp, "bantuan": CmdHelp,
		"apakah saya harus beli saham?": CmdQuestion, "bagaimana dengan emas": CmdQuestion, "???": CmdQuestion,
		"terima kasih": CmdUnknown, "": CmdUnknown, "   ": CmdUnknown, "stop sekarang juga": CmdUnknown,
	}
	for in, want := range tests {
		if got := ParseCommand(in); got != want {
			t.Errorf("%q -> %s want %s", in, got, want)
		}
	}
}

func fl(v float64) *float64 { return &v }

func full() Content {
	return Content{
		Name: "Surya", Risk: "Moderate", Ladder: 3,
		Alert: &Alert{Type: "asset_alert", Asset: "BBCA.JK", Direction: "down", ChangePct: -3.2, PositionIDR: -480000},
		News:  &News{Title: "BI Rate ditahan di 5,75%", Summary: "Bank Indonesia mempertahankan suku bunga acuan.", Reason: Text{"Anda punya reksa dana pendapatan tetap", "You hold a fixed-income fund"}, NumbersSafe: true, URL: "https://x.test/a"},
		Macro: &Macro{Label: Text{"USD/IDR", "USD/IDR"}, ValueText: "Rp 18.033", Change: fl(2.4), Reason: Text{"Anda punya aset dalam mata uang asing.", "You hold foreign-currency assets."}},
		Nudge: Nudge{Key: "investment", Action: Text{"Tingkatkan investasi bulanan.", "Increase your monthly investment."}, Reason: Text{"Anda investasi 13.3% dari income. Target: 20%. Tambah Rp 1.000.000/bulan.", "You invest 13.3% of income. Target: 20%. Add Rp 1.000.000/month."}},
	}
}

var opt = Options{Locale: "id", Link: "http://localhost:3000/dashboard", Now: time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)}

func TestComposeFullIDandEN(t *testing.T) {
	id := Compose(full(), opt)
	for _, want := range []string{"Selamat pagi, Surya. Berikut briefing keuangan Anda untuk 16 Okt.", "BBCA turun 3.2% kemarin.", "Posisi Anda turun ~Rp 480.000.", "Tidak ada aksi yang diperlukan saat ini.",
		"Berita: BI Rate ditahan", "Relevan karena Anda punya reksa dana pendapatan tetap.", "Makro: USD/IDR Rp 18.033 (naik 2.4%).", "Satu langkah: Tingkatkan investasi bulanan.", "Balas MORE", "http://localhost:3000/dashboard"} {
		if !strings.Contains(id.Text, want) {
			t.Errorf("id text lacks %q:\n%s", want, id.Text)
		}
	}
	if len(id.Slots) != 4 || id.CharCount != len([]rune(id.Text)) || id.CharCount > MaxTotal {
		t.Errorf("slots %d chars %d", len(id.Slots), id.CharCount)
	}
	en := Compose(full(), Options{Locale: "en", Link: opt.Link, Now: opt.Now})
	for _, want := range []string{"Good morning, Surya.", "16 Oct", "BBCA fell 3.2% yesterday.", "Your position fell ~Rp 480.000.", "Relevant because you hold a fixed-income fund.", "Macro: USD/IDR Rp 18.033 (up 2.4%).", "One step:"} {
		if !strings.Contains(en.Text, want) {
			t.Errorf("en text lacks %q:\n%s", want, en.Text)
		}
	}
	for _, r := range id.Text + en.Text {
		if r > 0x2000 && !unicode.IsLetter(r) { // no emoji or symbols outside ordinary punctuation
			t.Errorf("non-plain character %q", r)
		}
	}
}

func TestComposeSlotAndTotalLimits(t *testing.T) {
	long := strings.Repeat("kata panjang sekali ", 30)
	c := full()
	c.News.Title, c.News.Summary = long, long
	c.News.Reason = Text{long, long}
	c.Macro.Reason = Text{long, long}
	c.Nudge.Action, c.Nudge.Reason = Text{long, long}, Text{long, long}
	b := Compose(c, opt)
	if b.CharCount > MaxTotal {
		t.Fatalf("total %d", b.CharCount)
	}
	limits := map[string]int{SlotAlert: MaxSlot0, SlotNews: MaxSlot1, SlotMacro: MaxSlot2, SlotNudge: MaxSlot3}
	hasNudge := false
	for _, s := range b.Slots {
		if n := len([]rune(s.Text)); n > limits[s.Key] {
			t.Errorf("%s has %d > %d", s.Key, n, limits[s.Key])
		}
		hasNudge = hasNudge || s.Key == SlotNudge
	}
	if !hasNudge {
		t.Error("nudge is mandatory")
	}
	if len(b.Slots) > 4 {
		t.Error("max 4 slots")
	}
}

func TestComposeDropsLowestPriorityWhenOver800(t *testing.T) {
	c := full()
	// a very long name inflates the greeting so that the four slots no longer fit in 800 characters
	c.Name = strings.Repeat("N", 420)
	b := Compose(c, opt)
	if b.CharCount > MaxTotal {
		t.Fatalf("chars %d", b.CharCount)
	}
	if len(b.Dropped) == 0 || b.Dropped[0] != SlotMacro {
		t.Errorf("macro is dropped first: %v", b.Dropped)
	}
	if !strings.Contains(b.Text, "Satu langkah") {
		t.Error("nudge must survive")
	}
	for _, s := range b.Slots {
		for _, d := range b.Dropped {
			if s.Key == d {
				t.Errorf("%s is dropped but present", d)
			}
		}
	}
}

func TestComposeConditionalAndFallbacks(t *testing.T) {
	c := full()
	c.Alert = nil
	b := Compose(c, opt)
	if strings.Contains(b.Text, "BBCA") || len(b.Slots) != 3 {
		t.Errorf("alert slot should be skipped: %d slots", len(b.Slots))
	}
	n := Content{Name: "Surya", Nudge: full().Nudge}
	b = Compose(n, opt)
	if b.Fallback != "no_content_available" || !strings.Contains(b.Text, "Pasar tenang hari ini. Tidak ada berita penting.") || !strings.Contains(b.Text, "Detail: http://localhost:3000/dashboard") {
		t.Errorf("no content: %s", b.Text)
	}
	n.Degraded = true
	b = Compose(n, opt)
	if b.Fallback != "data_fetch_failure" || !strings.Contains(b.Text, "gangguan data") || !strings.Contains(b.Text, "Coba cek dashboard nanti") {
		t.Errorf("degraded: %s", b.Text)
	}
	n.Degraded, n.Quiet = false, true
	if b = Compose(n, opt); !strings.Contains(b.Text, "Ringkasan pasar") || b.Fallback != "" {
		t.Errorf("quiet market summary: %s", b.Text)
	}
	nu := full()
	nu.NewUser = true
	if b = Compose(nu, opt); !strings.Contains(b.Text, "Selamat datang, Surya!") {
		t.Errorf("new user: %s", b.Text)
	}
	r := full()
	r.Alert = &Alert{Type: "asset_alert", Asset: "TLKM.JK", Direction: "up", ChangePct: 3.5, Repeated: true}
	if b = Compose(r, opt); !strings.Contains(b.Text, "masih bergerak") || strings.Contains(b.Text, "Tidak ada aksi") {
		t.Errorf("repeated: %s", b.Text)
	}
	u := full()
	u.News.NumbersSafe, u.News.Followup = false, true
	if b = Compose(u, opt); strings.Contains(b.Text, "Bank Indonesia mempertahankan") || !strings.Contains(b.Text, "Update terbaru.") {
		t.Errorf("numbers-unsafe summary must be dropped, followup closes: %s", b.Text)
	}
}

func TestFooterFitsBudget(t *testing.T) {
	if f := footer("https://f.id/d", "id"); !strings.Contains(f, "Balas MORE untuk berita lain, STOP untuk berhenti.") || runes(f) > MaxFooter {
		t.Errorf("full footer %q (%d)", f, runes(f))
	}
	if f := footer("http://localhost:3000/dashboard", "en"); !strings.Contains(f, "Reply MORE or STOP.") {
		t.Errorf("short footer %q", f)
	}
}

func TestAlertVariants(t *testing.T) {
	for _, tc := range []struct {
		a    Alert
		want string
	}{
		{Alert{Type: "corporate_action", Asset: "TLKM.JK", Amount: 50}, "Aksi korporasi: TLKM dividen 50 per saham"},
		{Alert{Type: "52w_high_low", Asset: "ANTM.JK", Detail: "high"}, "ANTM mencapai harga tertinggi 52 minggu."},
		{Alert{Type: "52w_high_low", Asset: "ANTM.JK", Detail: "low"}, "terendah 52 minggu"},
	} {
		if got := alertSlot(tc.a, "Moderate", "id"); !strings.Contains(got, tc.want) {
			t.Errorf("%q lacks %q", got, tc.want)
		}
	}
	if got := alertSlot(Alert{Type: "asset_alert", Asset: "X", Direction: "down", ChangePct: -4, PositionIDR: -10}, "Aggressive", "id"); strings.Contains(got, "Tidak ada aksi") {
		t.Error("aggressive profile gets no reassurance")
	}
}

func TestReplyCommands(t *testing.T) {
	rc := ReplyContext{Locale: "id", Link: "http://x/dashboard", Content: Content{Risk: "Moderate", Ladder: 3, HealthTotal: 70.4, HealthCat: "Good",
		News: &News{Title: "Berita A"}, MoreNews: []News{{Title: "Berita B"}, {Title: "Berita C"}}}}
	for cmd, want := range map[Command]string{
		CmdMore: "Berita A", CmdStop: "briefing Anda dijeda", CmdResume: "aktif kembali", CmdProfile: "Risiko: Moderate", CmdUpdate: "perbarui profil", CmdHelp: "Command yang tersedia", CmdUnknown: "Command yang tersedia", CmdQuestion: "belum tersedia",
	} {
		if got := Reply(cmd, rc); !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %q", cmd, got, want)
		}
	}
	more := Reply(CmdMore, rc)
	if !strings.Contains(more, "Berita B") || strings.Contains(more, "Berita C") {
		t.Errorf("MORE shows two items: %s", more)
	}
	if !strings.Contains(Reply(CmdMore, ReplyContext{Locale: "id"}), "Belum ada berita lain") {
		t.Error("empty MORE")
	}
	rc.Locale = "en"
	if !strings.Contains(Reply(CmdStop, rc), "paused") || !strings.Contains(Reply(CmdProfile, rc), "Your profile") {
		t.Error("english replies")
	}
}

func TestDue(t *testing.T) {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	c := NewContact("u")
	c.Phone, c.Verified, c.OptIn = "+6281234567890", true, true
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, jkt) } // 16 Oct 2026 is a Friday
	tests := []struct {
		name string
		mut  func(*Contact)
		now  time.Time
		due  bool
	}{
		{"before send time", func(*Contact) {}, at(16, 6, 59), false},
		{"at send time", func(*Contact) {}, at(16, 7, 0), true},
		{"late but inside window", func(*Contact) {}, at(16, 12, 59), true},
		{"window over", func(*Contact) {}, at(16, 13, 0), false},
		{"utc clock converts to local", func(*Contact) {}, time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), true}, // 07:00 WIB
		{"not opted in", func(c *Contact) { c.OptIn = false }, at(16, 8, 0), false},
		{"not verified", func(c *Contact) { c.Verified = false }, at(16, 8, 0), false},
		{"stopped", func(c *Contact) { n := time.Now(); c.PausedAt = &n }, at(16, 8, 0), false},
		{"custom time", func(c *Contact) { c.SendTime = "18:30" }, at(16, 18, 30), true},
		{"weekdays on Friday", func(c *Contact) { c.Frequency = "weekdays" }, at(16, 8, 0), true},
		{"weekdays on Saturday", func(c *Contact) { c.Frequency = "weekdays" }, at(17, 8, 0), false},
		{"weekly on Friday", func(c *Contact) { c.Frequency = "weekly" }, at(16, 8, 0), false},
		{"weekly on Monday", func(c *Contact) { c.Frequency = "weekly" }, at(19, 8, 0), true},
		{"other timezone", func(c *Contact) { c.Timezone = "Asia/Makassar" }, at(16, 7, 0), false}, // 08:00 WITA is after 07:00 but local date same
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := c
			tc.mut(&x)
			date, due := Due(x, tc.now)
			if tc.name == "other timezone" {
				if !due || date != "2026-10-16" { // 07:00 WIB = 08:00 WITA: inside the window
					t.Errorf("%v %s", due, date)
				}
				return
			}
			if due != tc.due {
				t.Errorf("due=%v want %v", due, tc.due)
			}
			if due && date != "2026-10-16" && tc.name != "weekly on Monday" {
				t.Errorf("date %s", date)
			}
		})
	}
	if !ValidSendTime("07:00") || ValidSendTime("7:00") || ValidSendTime("24:00") || ValidSendTime("07:60") {
		t.Error("send time validation")
	}
}
