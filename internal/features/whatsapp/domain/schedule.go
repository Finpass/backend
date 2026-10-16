package domain

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrNotFound  = errors.New("whatsapp: not found")
	ErrDuplicate = errors.New("whatsapp: duplicate")
	ErrPhoneUsed = errors.New("whatsapp: phone verified by another account")
)

const (
	VerifyTTL         = 10 * time.Minute
	VerifyMaxAttempts = 5
	VerifyCooldown    = 60 * time.Second
	MaxRetries        = 3
	RetryInterval     = 5 * time.Minute
	maxLate           = 6 * time.Hour
	DefaultSendTime   = "07:00"
	DefaultTimezone   = "Asia/Jakarta"
)

var timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func ValidSendTime(s string) bool { return timeRe.MatchString(s) }

func ValidFrequency(f string) bool { return f == "daily" || f == "weekdays" || f == "weekly" }

func ValidLocale(l string) bool { return l == "id" || l == "en" }

func LoadTimezone(name string) (*time.Location, error) { return time.LoadLocation(name) }

type Contact struct {
	UserID        string
	Phone         string
	Verified      bool
	VerifiedAt    *time.Time
	OptIn         bool
	PausedAt      *time.Time
	SendTime      string
	Timezone      string
	Frequency     string
	Locale        string
	LastSentAt    *time.Time
	LastInboundAt *time.Time
	FailStreak    int
}

func NewContact(userID string) Contact {
	return Contact{UserID: userID, SendTime: DefaultSendTime, Timezone: DefaultTimezone, Frequency: "daily", Locale: "id"}
}

func (c Contact) Location() *time.Location {
	if l, err := time.LoadLocation(c.Timezone); err == nil {
		return l
	}
	l, _ := time.LoadLocation(DefaultTimezone)
	return l
}

// Active reports whether the contact may receive scheduled briefings at all (opt-in, verified, not stopped).
func (c Contact) Active() bool { return c.Phone != "" && c.Verified && c.OptIn && c.PausedAt == nil }

// Due returns the user-local date to run for and whether now falls in the send window
// [sendTime, sendTime+6h) on a day matching the frequency (weekdays: Mon-Fri, weekly: Monday).
func Due(c Contact, now time.Time) (string, bool) {
	if !c.Active() || !ValidSendTime(c.SendTime) {
		return "", false
	}
	l := now.In(c.Location())
	switch c.Frequency {
	case "weekdays":
		if l.Weekday() == time.Saturday || l.Weekday() == time.Sunday {
			return "", false
		}
	case "weekly":
		if l.Weekday() != time.Monday {
			return "", false
		}
	}
	h, m := int(c.SendTime[0]-'0')*10+int(c.SendTime[1]-'0'), int(c.SendTime[3]-'0')*10+int(c.SendTime[4]-'0')
	start := time.Date(l.Year(), l.Month(), l.Day(), h, m, 0, 0, l.Location())
	if l.Before(start) || !l.Before(start.Add(maxLate)) {
		return "", false
	}
	return l.Format("2006-01-02"), true
}

type Verification struct {
	UserID    string
	Phone     string
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
	CreatedAt time.Time
}
