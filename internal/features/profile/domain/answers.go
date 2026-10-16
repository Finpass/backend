package domain

import (
	"math"
	"slices"

	"shenzen/backend/internal/shared/apperr"
)

// Answers are JSON-shaped: strings, whole-number float64 and []any of strings.
type Answers map[string]any

var optOutQuestions = []string{"q2", "q7"}

func (a Answers) str(k string) string { s, _ := a[k].(string); return s }

func (a Answers) num(k string) (int64, bool) {
	f, ok := a[k].(float64)
	return int64(f), ok
}

func (a Answers) list(k string) ([]string, bool) {
	raw, ok := a[k].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out, true
}

func bad(field, msg string) apperr.Detail { return apperr.Detail{Field: field, Message: msg} }

// ValidateAnswers checks only the provided keys. Nil values mean "clear" and are skipped.
func ValidateAnswers(a Answers, optOut []string) []apperr.Detail {
	var out []apperr.Detail
	for _, k := range slices.Sorted(mapKeys(a)) {
		if a[k] == nil {
			continue
		}
		spec, ok := specByID[k]
		if !ok {
			out = append(out, bad(k, "unknown_question"))
			continue
		}
		if msg := checkValue(spec, a[k]); msg != "" {
			out = append(out, bad(k, msg))
		}
	}
	for _, k := range optOut {
		switch {
		case !slices.Contains(optOutQuestions, k):
			out = append(out, bad(k, "opt_out_not_allowed"))
		case a[k] != nil:
			out = append(out, bad(k, "opt_out_conflicts_with_answer"))
		}
	}
	return out
}

func mapKeys(a Answers) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range a {
			if !yield(k) {
				return
			}
		}
	}
}

func checkValue(q Question, v any) string {
	switch q.Type {
	case TypeSingle:
		s, ok := v.(string)
		if !ok || !hasOption(q, s) {
			return "invalid_option"
		}
	case TypeMulti:
		raw, ok := v.([]any)
		if !ok || len(raw) == 0 {
			return "must_be_non_empty_list"
		}
		seen := map[string]bool{}
		for _, e := range raw {
			s, ok := e.(string)
			if !ok || !hasOption(q, s) {
				return "invalid_option"
			}
			if seen[s] {
				return "duplicate_option"
			}
			seen[s] = true
		}
		if seen[q.ExclusiveOption] && len(raw) > 1 {
			return "exclusive_option"
		}
	case TypeNumeric:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return "must_be_integer"
		}
		if f < float64(*q.Min) {
			return "below_min"
		}
		if f > float64(*q.Max) {
			return "above_max"
		}
	}
	return ""
}

func hasOption(q Question, id string) bool {
	return slices.ContainsFunc(q.Options, func(o Option) bool { return o.ID == id })
}

// Missing lists required questions with neither an answer nor an opt-out, in wizard order.
func Missing(a Answers, optOut []string) []string {
	out := []string{}
	for _, id := range order {
		q := specByID[id]
		var required bool
		if id == "q6_installment" {
			required = hasDebt(a)
		} else {
			required = q.Required
		}
		if !required || slices.Contains(optOut, id) {
			continue
		}
		if _, ok := a[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func hasDebt(a Answers) bool {
	d, ok := a.list("q6")
	return ok && !slices.Contains(d, "none")
}

// CheckInvestable enforces q8 <= q2 - q3 - installment. A negative surplus allows only 0 so such a user can still submit.
func CheckInvestable(a Answers, optOut []string) []apperr.Detail {
	q8, ok8 := a.num("q8")
	q2, ok2 := a.num("q2")
	q3, ok3 := a.num("q3")
	if !ok8 || !ok2 || !ok3 || slices.Contains(optOut, "q2") {
		return nil
	}
	inst, _ := a.num("q6_installment")
	if !hasDebt(a) {
		inst = 0
	}
	if q8 > max(0, q2-q3-inst) {
		return []apperr.Detail{bad("q8", "exceeds_surplus")}
	}
	return nil
}

// Merge applies a partial update to a draft. section scopes the opt-out replacement; optIn nil leaves opt-outs alone.
func Merge(cur Answers, curOpt []string, in Answers, inOpt []string, section string) (Answers, []string) {
	out := Answers{}
	for k, v := range cur {
		out[k] = v
	}
	for k, v := range in {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	opt := slices.Clone(curOpt)
	if opt == nil {
		opt = []string{}
	}
	if inOpt != nil {
		opt = slices.DeleteFunc(opt, func(k string) bool { return section == "" || sectionOf[k] == section })
		opt = append(opt, inOpt...)
	}
	for k, v := range in {
		if v != nil { // an explicit answer replaces an earlier opt-out
			opt = slices.DeleteFunc(opt, func(o string) bool { return o == k })
		}
	}
	opt = append([]string{}, slices.Compact(slices.SortedFunc(slices.Values(opt), func(a, b string) int {
		return slices.Index(order, a) - slices.Index(order, b)
	}))...)
	for _, k := range opt {
		delete(out, k)
	}
	if !hasDebt(out) {
		delete(out, "q6_installment")
	}
	return out, opt
}
