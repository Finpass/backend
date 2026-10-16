package domain

import (
	"slices"
	"testing"
	"time"
)

func errFields(a Answers, optOut []string) []string {
	var out []string
	for _, d := range ValidateAnswers(a, optOut) {
		out = append(out, d.Field+":"+d.Message)
	}
	return out
}

func TestValidateAnswers(t *testing.T) {
	cases := []struct {
		name   string
		a      Answers
		optOut []string
		want   []string
	}{
		{"valid single", Answers{"q1": "25_30"}, nil, nil},
		{"unknown option", Answers{"q1": "25-30"}, nil, []string{"q1:invalid_option"}},
		{"single given a number", Answers{"q1": 3.0}, nil, []string{"q1:invalid_option"}},
		{"unknown question", Answers{"q99": "x"}, nil, []string{"q99:unknown_question"}},
		{"numeric ok", Answers{"q2": 15_000_000.0}, nil, nil},
		{"numeric zero ok", Answers{"q3": 0.0}, nil, nil},
		{"numeric negative", Answers{"q2": -1.0}, nil, []string{"q2:below_min"}},
		{"q2 above max", Answers{"q2": 1_000_000_001.0}, nil, []string{"q2:above_max"}},
		{"q2 at max", Answers{"q2": 1_000_000_000.0}, nil, nil},
		{"q4 max is 100B", Answers{"q4": 100_000_000_000.0}, nil, nil},
		{"q4 above 100B", Answers{"q4": 100_000_000_001.0}, nil, []string{"q4:above_max"}},
		{"q7 max is 1T", Answers{"q7": 1_000_000_000_000.0}, nil, nil},
		{"fraction", Answers{"q2": 1.5}, nil, []string{"q2:must_be_integer"}},
		{"numeric as string", Answers{"q2": "15jt"}, nil, []string{"q2:must_be_integer"}},
		{"multi ok", Answers{"q6": []any{"pinjol", "kta"}}, nil, nil},
		{"multi none alone", Answers{"q6": []any{"none"}}, nil, nil},
		{"multi none is exclusive", Answers{"q6": []any{"none", "kta"}}, nil, []string{"q6:exclusive_option"}},
		{"multi empty", Answers{"q6": []any{}}, nil, []string{"q6:must_be_non_empty_list"}},
		{"multi duplicate", Answers{"q23": []any{"gold", "gold"}}, nil, []string{"q23:duplicate_option"}},
		{"multi bad option", Answers{"q23": []any{"nft"}}, nil, []string{"q23:invalid_option"}},
		{"multi not a list", Answers{"q23": "gold"}, nil, []string{"q23:must_be_non_empty_list"}},
		{"followup range", Answers{"q6_installment": 2_000_000_000.0}, nil, []string{"q6_installment:above_max"}},
		{"nil clears", Answers{"q2": nil}, nil, nil},
		{"opt out q2", Answers{}, []string{"q2"}, nil},
		{"opt out q7", Answers{}, []string{"q7"}, nil},
		{"opt out not allowed", Answers{}, []string{"q3"}, []string{"q3:opt_out_not_allowed"}},
		{"opt out with value", Answers{"q2": 1.0}, []string{"q2"}, []string{"q2:opt_out_conflicts_with_answer"}},
		{"several errors sorted", Answers{"q2": -5.0, "q1": "x"}, nil, []string{"q1:invalid_option", "q2:below_min"}},
	}
	for _, c := range cases {
		got := errFields(c.a, c.optOut)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMissing(t *testing.T) {
	if got := Missing(Answers{}, nil); len(got) != 23 || got[0] != "q1" || slices.Contains(got, "q16b") || slices.Contains(got, "q6_installment") {
		t.Errorf("empty draft: %v", got)
	}
	full := dindaAnswers()
	if got := Missing(full, nil); len(got) != 0 {
		t.Errorf("complete draft still missing %v", got)
	}
	delete(full, "q6_installment")
	if got := Missing(full, nil); !slices.Equal(got, []string{"q6_installment"}) {
		t.Errorf("debt without installment: %v", got)
	}
	full["q6"] = []any{"none"}
	if got := Missing(full, nil); len(got) != 0 {
		t.Errorf("no debt needs no installment: %v", got)
	}
	delete(full, "q2")
	delete(full, "q7")
	if got := Missing(full, []string{"q2", "q7"}); len(got) != 0 {
		t.Errorf("opted out questions are not missing: %v", got)
	}
	if got := Missing(full, nil); !slices.Equal(got, []string{"q2", "q7"}) {
		t.Errorf("got %v", got)
	}
}

func TestCheckInvestable(t *testing.T) {
	mk := func(q2, q3, q8, inst float64, debt bool) Answers {
		a := Answers{"q2": q2, "q3": q3, "q8": q8, "q6": []any{"none"}}
		if debt {
			a["q6"], a["q6_installment"] = []any{"kta"}, inst
		}
		return a
	}
	cases := []struct {
		name string
		a    Answers
		opt  []string
		bad  bool
	}{
		{"within surplus", mk(8e6, 5e6, 3e6, 0, false), nil, false},
		{"one rupiah over", mk(8e6, 5e6, 3_000_001, 0, false), nil, true},
		{"installment reduces room", mk(8e6, 5e6, 2_500_001, 5e5, true), nil, true},
		{"installment exactly fits", mk(8e6, 5e6, 2_500_000, 5e5, true), nil, false},
		{"no debt ignores stale installment", Answers{"q2": 8e6, "q3": 5e6, "q8": 3e6, "q6": []any{"none"}, "q6_installment": 9e6}, nil, false},
		{"negative surplus allows zero", mk(5e6, 6e6, 0, 0, false), nil, false},
		{"negative surplus rejects any investing", mk(5e6, 6e6, 1, 0, false), nil, true},
		{"income opted out skips the check", Answers{"q3": 5e6, "q8": 9e9}, []string{"q2"}, false},
		{"missing q3 skips the check", Answers{"q2": 1e6, "q8": 9e9}, nil, false},
	}
	for _, c := range cases {
		got := CheckInvestable(c.a, c.opt)
		if (len(got) > 0) != c.bad || (c.bad && got[0].Field != "q8") {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestMerge(t *testing.T) {
	cur := Answers{"q1": "25_30", "q2": 5e6, "q6": []any{"kta"}, "q6_installment": 1e5}
	t.Run("overwrites and keeps the rest", func(t *testing.T) {
		got, opt := Merge(cur, nil, Answers{"q1": "31_40"}, nil, "")
		if opt == nil {
			t.Error("opt out must serialise as [] not null")
		}
		if got["q1"] != "31_40" || got["q2"] != 5e6 || len(got) != len(cur) {
			t.Errorf("got %v", got)
		}
		if cur["q1"] != "25_30" {
			t.Error("merge mutated its input")
		}
	})
	t.Run("nil deletes", func(t *testing.T) {
		got, _ := Merge(cur, nil, Answers{"q2": nil}, nil, "")
		if _, ok := got["q2"]; ok {
			t.Error("q2 should be cleared")
		}
	})
	t.Run("switching to no debt drops the installment", func(t *testing.T) {
		got, _ := Merge(cur, nil, Answers{"q6": []any{"none"}}, nil, "")
		if _, ok := got["q6_installment"]; ok {
			t.Error("stale installment kept")
		}
	})
	t.Run("opt out removes the answer", func(t *testing.T) {
		got, opt := Merge(cur, nil, Answers{}, []string{"q2"}, "section_2")
		if _, ok := got["q2"]; ok || !slices.Equal(opt, []string{"q2"}) {
			t.Errorf("answers %v opt %v", got, opt)
		}
	})
	t.Run("an answer lifts the opt out", func(t *testing.T) {
		_, opt := Merge(Answers{}, []string{"q2", "q7"}, Answers{"q2": 1e6}, nil, "")
		if !slices.Equal(opt, []string{"q7"}) {
			t.Errorf("opt %v", opt)
		}
	})
	t.Run("section scoped replacement leaves other sections alone", func(t *testing.T) {
		_, opt := Merge(Answers{}, []string{"q2", "q7"}, Answers{}, []string{}, "section_2")
		if !slices.Equal(opt, []string{"q7"}) {
			t.Errorf("opt %v", opt)
		}
	})
	t.Run("no section replaces everything", func(t *testing.T) {
		_, opt := Merge(Answers{}, []string{"q2", "q7"}, Answers{}, []string{"q7"}, "")
		if !slices.Equal(opt, []string{"q7"}) {
			t.Errorf("opt %v", opt)
		}
	})
	t.Run("nil opt out is untouched and deduplicated sorted", func(t *testing.T) {
		_, opt := Merge(Answers{}, []string{"q7", "q2"}, Answers{}, nil, "")
		if !slices.Equal(opt, []string{"q2", "q7"}) {
			t.Errorf("opt %v", opt)
		}
	})
}

func TestInputFromHonoursOptOutAndNone(t *testing.T) {
	a := dindaAnswers()
	a["q6"] = []any{"none"}
	delete(a, "q6_installment")
	delete(a, "q2")
	delete(a, "q7")
	in := InputFrom(a, []string{"q2", "q7"})
	if in.IncomeKnown || in.Income != 0 || in.Investments != 0 || in.hasDebt() || in.Installment != 0 {
		t.Errorf("%+v", in)
	}
	a["q23"] = []any{"none"}
	if in := InputFrom(a, nil); in.Holdings == nil || len(in.Holdings) != 0 {
		t.Errorf("none should be answered-but-empty, got %#v", in.Holdings)
	}
	if got := dinda(); got.Q12 != 2 || got.Q13 != 1 || got.Q14 != 1 || got.Q15 != 1 || got.Knowledge != 1 || !got.Q16bCorrect {
		t.Errorf("option scores wrong: %+v", got)
	}
}

func TestCanRetake(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	ago := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	cases := []struct {
		name      string
		history   []time.Time
		allowed   bool
		remaining int
		nextIn    int // days from now, when not allowed
	}{
		{"first assessment", nil, true, 3, 0},
		{"day 29", []time.Time{ago(29)}, false, 2, 1},
		{"day 30", []time.Time{ago(30)}, true, 2, 0},
		{"day 31", []time.Time{ago(31)}, true, 2, 0},
		{"three in a year, cooldown over", []time.Time{ago(200), ago(120), ago(60)}, false, 0, 165},
		{"three but the oldest expires soon", []time.Time{ago(360), ago(100), ago(40)}, false, 0, 5},
		{"old ones fall out of the window", []time.Time{ago(400), ago(380), ago(366), ago(40)}, true, 2, 0},
		{"two in a year", []time.Time{ago(300), ago(100)}, true, 1, 0},
	}
	for _, c := range cases {
		st := CanRetake(now, c.history)
		if st.Allowed != c.allowed || st.RemainingThisYear != c.remaining {
			t.Errorf("%s: %+v", c.name, st)
			continue
		}
		if !c.allowed {
			if st.NextAllowedAt == nil || !st.NextAllowedAt.Equal(now.Add(time.Duration(c.nextIn)*24*time.Hour)) {
				t.Errorf("%s: next = %v, want %d days out", c.name, st.NextAllowedAt, c.nextIn)
			}
		} else if st.NextAllowedAt != nil {
			t.Errorf("%s: next should be nil", c.name)
		}
	}
}

func TestQuestionnaireMatchesContract(t *testing.T) {
	if Spec.Version != QuestionnaireVersion || len(Spec.Sections) != 6 {
		t.Fatalf("version %s sections %d", Spec.Version, len(Spec.Sections))
	}
	want := map[string][]string{
		"section_1": {"q1", "q5", "q10", "q11"}, "section_2": {"q2", "q3", "q4", "q6"}, "section_3": {"q7", "q8", "q9", "q23"},
		"section_4": {"q12", "q13", "q14", "q15", "q16", "q16b"}, "section_5": {"q17", "q18", "q19"}, "section_6": {"q20", "q21", "q22"},
	}
	for _, s := range Spec.Sections {
		var ids []string
		for _, q := range s.Questions {
			ids = append(ids, q.ID)
			if q.TextEn == "" || q.TextID == "" {
				t.Errorf("%s lacks text", q.ID)
			}
			for _, o := range q.Options {
				if o.LabelEn == "" || o.LabelID == "" {
					t.Errorf("%s/%s lacks a label", q.ID, o.ID)
				}
			}
		}
		if !slices.Equal(ids, want[s.ID]) {
			t.Errorf("%s: %v, want %v", s.ID, ids, want[s.ID])
		}
	}
	// Every option the engine scores must exist in the questionnaire, otherwise a valid answer would score as zero.
	tables := map[string][]string{
		"q1": keysOf(ageCapacity), "q5": keysOf(depCapacity), "q10": keysOf(employmentBase), "q11": keysOf(stabilityMod),
		"q9": keysOf(horizonScore), "q19": keysOf(timelineMonths), "q20": keysOf(homeStability), "q21": keysOf(priorExpScore),
	}
	for qid, keys := range tables {
		for _, k := range keys {
			if !hasOption(specByID[qid], k) && !(qid == "q19" && k == "none") {
				t.Errorf("%s: engine table key %q is not an option", qid, k)
			}
		}
	}
	for _, q := range []string{"q12", "q13", "q14", "q15", "q16"} {
		if n := len(specByID[q].Options); n != 4 {
			t.Errorf("%s has %d options, the score is the index so it needs 4", q, n)
		}
	}
	if specByID["q16b"].Required || specByID["q16b"].Options[0].ID != "a" {
		t.Error("q16b must be optional with option a first")
	}
}

func keysOf[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
