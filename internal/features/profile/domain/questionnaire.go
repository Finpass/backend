package domain

import (
	_ "embed"
	"encoding/json"
	"slices"
)

const (
	QuestionnaireVersion = "4.0"
	RuleVersion          = "4.0"
)

//go:embed questionnaire.json
var questionnaireJSON []byte

type Option struct {
	ID      string `json:"id"`
	LabelEn string `json:"labelEn"`
	LabelID string `json:"labelId"`
}

type ShowWhen struct {
	QID   string   `json:"qid"`
	NotIn []string `json:"notIn"`
}

type Followup struct {
	ID       string   `json:"id"`
	TextEn   string   `json:"textEn"`
	TextID   string   `json:"textId"`
	Unit     string   `json:"unit"`
	Min      int64    `json:"min"`
	Max      int64    `json:"max"`
	ShowWhen ShowWhen `json:"showWhen"`
}

type Question struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Required        bool      `json:"required"`
	TextEn          string    `json:"textEn"`
	TextID          string    `json:"textId"`
	HintEn          string    `json:"hintEn,omitempty"`
	HintID          string    `json:"hintId,omitempty"`
	Unit            string    `json:"unit,omitempty"`
	Min             *int64    `json:"min,omitempty"`
	Max             *int64    `json:"max,omitempty"`
	OptOutAllowed   bool      `json:"optOutAllowed,omitempty"`
	ExclusiveOption string    `json:"exclusiveOption,omitempty"`
	Options         []Option  `json:"options,omitempty"`
	Followup        *Followup `json:"followup,omitempty"`
}

type Section struct {
	ID           string     `json:"id"`
	NameEn       string     `json:"nameEn"`
	NameID       string     `json:"nameId"`
	Icon         string     `json:"icon"`
	DisclaimerEn string     `json:"disclaimerEn,omitempty"`
	DisclaimerID string     `json:"disclaimerId,omitempty"`
	Questions    []Question `json:"questions"`
}

type Questionnaire struct {
	Version  string    `json:"version"`
	Sections []Section `json:"sections"`
}

const (
	TypeSingle  = "single_choice"
	TypeMulti   = "multi_select"
	TypeNumeric = "numeric"
)

var (
	Spec      = mustLoad()
	specByID  = map[string]Question{}
	order     []string // every answerable id in wizard order, followups right after their parent
	sectionOf = map[string]string{}
)

func mustLoad() Questionnaire {
	var q Questionnaire
	if err := json.Unmarshal(questionnaireJSON, &q); err != nil {
		panic("profile: invalid embedded questionnaire: " + err.Error())
	}
	for _, s := range q.Sections {
		for _, qu := range s.Questions {
			specByID[qu.ID] = qu
			order = append(order, qu.ID)
			sectionOf[qu.ID] = s.ID
			if f := qu.Followup; f != nil {
				lo, hi := f.Min, f.Max
				specByID[f.ID] = Question{ID: f.ID, Type: TypeNumeric, Min: &lo, Max: &hi}
				order = append(order, f.ID)
				sectionOf[f.ID] = s.ID
			}
		}
	}
	return q
}

func SectionExists(id string) bool { return slices.Contains(sectionIDs(), id) }

func sectionIDs() []string {
	ids := make([]string, len(Spec.Sections))
	for i, s := range Spec.Sections {
		ids[i] = s.ID
	}
	return ids
}

func FirstSection() string { return Spec.Sections[0].ID }

// optionIndex is the 0-based position of an option, which is its score for q12..q16.
func optionIndex(qid, opt string) int {
	for i, o := range specByID[qid].Options {
		if o.ID == opt {
			return i
		}
	}
	return 0
}
