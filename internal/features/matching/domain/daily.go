package domain

import "time"

type DailyInput struct {
	Profile  Profile
	Holdings []Holding
	Articles []Article
	Macro    []MacroIndicator
	History  []DayRecord
	Now      time.Time // user-local time; its date is "today"
}

type DailyTrace struct {
	Alerts []AlertTrace `json:"alerts"`
	News   []NewsTrace  `json:"news"`
	Macro  []MacroTrace `json:"macro"`
	Nudge  []string     `json:"nudge"`
}

type DailyOutput struct {
	Alert       *Alert     `json:"alert"`
	News        *NewsPick  `json:"news"`
	MoreNews    []NewsPick `json:"moreNews"`
	Macro       *MacroPick `json:"macro"`
	Nudge       Nudge      `json:"nudge"`
	QuietMarket bool       `json:"quietMarket"`
	Trace       DailyTrace `json:"trace"`
}

// MatchDaily runs the four layers.
func MatchDaily(in DailyInput) DailyOutput {
	var o DailyOutput
	today := dayStart(in.Now)
	o.Alert, o.Trace.Alerts = MatchAlert(in.Holdings, today, in.History)
	o.News, o.MoreNews, o.Trace.News = MatchNews(in.Profile, in.Holdings, in.Articles, in.History, in.Now)
	o.Macro, o.Trace.Macro = MatchMacro(in.Profile, in.Holdings, in.Macro, in.Now)
	o.Nudge, o.Trace.Nudge = MatchNudge(in.Profile, in.History)
	o.QuietMarket = QuietMarket(in.History, o.Alert != nil)
	if o.MoreNews == nil {
		o.MoreNews = []NewsPick{}
	}
	return o
}
