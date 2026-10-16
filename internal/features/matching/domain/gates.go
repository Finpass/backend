package domain

// Exclusion records why a candidate was filtered (the trace asked for by the YAML).
type Exclusion struct {
	Symbol string `json:"symbol"`
	Gate   string `json:"gate"`
	Reason string `json:"reason"`
}

// GateInput are the user constraints; q25/q26 do not exist in questionnaire v4 so those gates are reported as not applicable.
type GateNotes []string

// CheckGates applies layer 2 to one candidate; it returns the first failing gate or "" when eligible.
func CheckGates(p Profile, c Candidate) (gate, reason string) {
	if p.Sharia == "yes" && c.Sharia != "sharia_compliant" {
		if c.Sharia == "non_sharia" {
			return "gate_sharia", "q22 = yes and asset is non_sharia"
		}
		return "gate_sharia", "q22 = yes and sharia status is " + c.Sharia + " (cannot be verified, excluded)"
	}
	h := p.HorizonYears()
	if h < 3 && c.Volatility == "high" {
		return "gate_horizon", "horizon < 3 years and volatility is high"
	}
	if h < 1 && c.Volatility == "medium" {
		return "gate_horizon", "horizon < 1 year and volatility is medium"
	}
	if p.Risk == Conservative && c.Volatility == "high" {
		return "gate_volatility", "Conservative profile and volatility is high"
	}
	switch p.Knowledge {
	case "none":
		if c.Class == ClassCrypto || c.Class == ClassMidSmall {
			return "gate_knowledge", "q16 = none excludes crypto and small cap"
		}
	case "basic":
		if c.Class == ClassMidSmall {
			return "gate_knowledge", "q16 = basic excludes small cap"
		}
	}
	if p.Ladder <= 2 {
		switch c.Class {
		case ClassDeposit, ClassFund, ClassBondGov:
		default:
			return "gate_ladder", "ladder_level <= 2 allows only deposits, money-market funds and government bonds"
		}
	}
	return "", ""
}

// GateNotesFor lists the gates that cannot be evaluated with the questionnaire answers available.
func GateNotesFor() GateNotes {
	return GateNotes{
		"gate_exclusion: q26 is not part of questionnaire v4, not applied",
		"gate_liquidity: q25 is not part of questionnaire v4, not applied",
		"gate_geography: q24 is not part of questionnaire v4, not applied",
	}
}
