package domain

// Lookup tables from YAML rule_engine v4.0. Option ids follow the shared contract.
var (
	ageCapacity    = map[string]float64{"18_24": 100, "25_30": 90, "31_40": 75, "41_50": 60, "51_60": 40, "60_plus": 20}
	depCapacity    = map[string]float64{"0": 100, "1": 80, "2": 60, "3": 40, "4_plus": 20}
	employmentBase = map[string]float64{"permanent_employee": 100, "contract_employee": 70, "freelancer": 55, "business_owner": 50, "student": 30, "housewife": 50, "retired": 40}
	stabilityMod   = map[string]float64{"very_stable": 10, "somewhat_stable": 0, "unstable": -15, "no_fixed_income": -25}
	horizonScore   = map[string]float64{"gt_10": 100, "5_10": 80, "3_5": 60, "1_3": 40, "lt_1": 20, "none": 30}

	// Step 1 emergency fund targets (min, max months) and Step 7 safety targets differ in the YAML.
	capacityTarget = map[string]MonthsRange{
		"permanent_employee": {3, 6}, "contract_employee": {6, 6}, "freelancer": {9, 9}, "business_owner": {9, 12},
		"student": {3, 3}, "housewife": {6, 6}, "retired": {12, 12},
	}
	safetyTarget = map[string]float64{"permanent_employee": 6, "contract_employee": 6, "freelancer": 9, "business_owner": 12, "student": 3, "housewife": 6, "retired": 12}

	ageGrowthMod = map[string]float64{"18_24": 5, "25_30": 0, "31_40": 0, "41_50": -5, "51_60": -10, "60_plus": -15}
	ageSafetyMod = map[string]float64{"18_24": 0, "25_30": 0, "31_40": 5, "41_50": 10, "51_60": 10, "60_plus": 15}
	depFreedom   = map[string]float64{"0": 0, "1": -3, "2": -5, "3": -8, "4_plus": -10}

	// Months for q19 buckets when computing the monthly saving a goal needs (bucket midpoints).
	timelineMonths  = map[string]float64{"lt_1": 6, "1_3": 24, "3_5": 48, "5_10": 90, "gt_10": 180}
	timelineQuality = map[string]float64{"lt_1": 40, "1_3": 60, "3_5": 80, "5_10": 90, "gt_10": 100}
	homeStability   = map[string]float64{"own_outright": 100, "own_mortgage": 70, "living_with_family": 50, "rent": 40}
	knowledgeScore  = [4]float64{0, 33, 66, 100}
	priorExpScore   = map[string]float64{"never": 0, "yes_not_active": 50, "yes_active": 100}
)

type tier struct{ min, score float64 }

// lookup returns the score of the first tier whose lower bound v reaches; tiers go from high to low.
func lookup(v float64, tiers []tier, floor float64) float64 {
	for _, t := range tiers {
		if v >= t.min {
			return t.score
		}
	}
	return floor
}

var (
	savingsRateTiers = []tier{{0.30, 100}, {0.20, 80}, {0.10, 60}, {0, 35}}
	investableTiers  = []tier{{0.20, 100}, {0.10, 75}, {0.05, 50}}
	efBaseTiers      = []tier{{12, 100}, {6, 85}, {3, 65}, {1, 35}}
	assetTiers       = []tier{{3, 90}, {1, 75}, {0.5, 50}}
	feasibilityTiers = []tier{{1, 100}, {0.7, 75}, {0.5, 50}, {0.3, 25}}
)
