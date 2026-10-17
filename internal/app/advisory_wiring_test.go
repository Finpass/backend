package app

import (
	"encoding/json"
	"strings"
	"testing"

	matchapp "shenzen/backend/internal/features/matching/application"
	matchdomain "shenzen/backend/internal/features/matching/domain"
	pfdomain "shenzen/backend/internal/features/portfolio/domain"
)

func TestUserContextHasNoPersonalData(t *testing.T) {
	p := matchdomain.Profile{UserID: "uid-secret", Name: "Budi", Risk: "Moderate", Ladder: 3, Sharia: "prefer", GoalHorizon: "3_5", Goal: "wealth_growth"}
	prod := matchapp.ProductResult{}
	prod.Portfolio = []matchdomain.ProductClass{{Class: "equity_large_cap", Allocation: 25.4, Assets: []matchdomain.ProductAsset{{Ticker: "ULTJ.JK", Weight: 8.8}}}}
	sym := "BBCA.JK"
	hs := []pfdomain.HoldingView{{Symbol: &sym, Name: "BCA", AssetClass: "equity", MarketValue: 300}, {Name: "Emas", AssetClass: "gold", MarketValue: 100}}
	uc := buildUserContext(p, prod, hs)
	raw, _ := json.Marshal(uc)
	for _, bad := range []string{"Budi", "uid-secret", "email", "phone", "name\":\"Budi"} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("leaked %q in %s", bad, raw)
		}
	}
	if uc.Holdings[0].WeightPct != 75 || uc.Holdings[1].WeightPct != 25 || uc.Profile.HorizonYears != 4 || *uc.Profile.Goal != "wealth_growth" || uc.Recommended[0].Assets[0].Ticker != "ULTJ.JK" {
		t.Fatalf("%s", raw)
	}
}

func TestNewUUIDShape(t *testing.T) {
	u := newUUID()
	if len(u) != 36 || u[14] != '4' || u[8] != '-' || u[23] != '-' {
		t.Fatal(u)
	}
}
