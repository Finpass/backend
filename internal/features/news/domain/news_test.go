package domain

import "testing"

func TestRelevant(t *testing.T) {
	cases := []struct {
		tickers, held []string
		want          bool
	}{
		{[]string{"AAPL"}, []string{"AAPL"}, true},
		{[]string{"BBCA"}, []string{"BBCA.JK"}, true},
		{[]string{"bbca.jk"}, []string{"BBCA"}, true},
		{[]string{"NVDA", "AAPL"}, []string{"BBCA.JK", "aapl"}, true},
		{[]string{"NVDA"}, []string{"AAPL"}, false},
		{nil, []string{"AAPL"}, false},
		{[]string{"AAPL"}, nil, false},
	}
	for _, c := range cases {
		if got := Relevant(c.tickers, c.held); got != c.want {
			t.Errorf("%v vs %v = %v", c.tickers, c.held, got)
		}
	}
}

func TestSentimentLabel(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for in, want := range map[float64]string{0.9: "positive", 0.15: "positive", 0: "neutral", -0.1: "neutral", -0.15: "negative", -1: "negative"} {
		if got := SentimentLabel(f(in)); *got != want {
			t.Errorf("%v: %v", in, *got)
		}
	}
	if SentimentLabel(nil) != nil {
		t.Fatal("nil stays nil")
	}
}
