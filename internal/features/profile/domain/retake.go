package domain

import (
	"slices"
	"time"
)

const (
	RetakeCooldown   = 30 * 24 * time.Hour
	RetakesPerWindow = 3
	RetakeWindow     = 365 * 24 * time.Hour
)

type RetakeStatus struct {
	Allowed           bool       `json:"allowed"`
	NextAllowedAt     *time.Time `json:"nextAllowedAt"`
	RemainingThisYear int        `json:"remainingThisYear"`
}

// CanRetake applies the 30 day cooldown and the cap of 3 assessments per rolling year to past assessment times.
func CanRetake(now time.Time, history []time.Time) RetakeStatus {
	var recent []time.Time
	for _, t := range history {
		if t.After(now.Add(-RetakeWindow)) {
			recent = append(recent, t)
		}
	}
	slices.SortFunc(recent, func(a, b time.Time) int { return b.Compare(a) }) // newest first
	st := RetakeStatus{RemainingThisYear: max(0, RetakesPerWindow-len(recent))}
	var next time.Time
	if len(recent) > 0 {
		next = recent[0].Add(RetakeCooldown)
	}
	if len(recent) >= RetakesPerWindow {
		if free := recent[RetakesPerWindow-1].Add(RetakeWindow); free.After(next) {
			next = free
		}
	}
	if next.After(now) {
		next = next.UTC()
		st.NextAllowedAt = &next
		return st
	}
	st.Allowed = true
	return st
}
