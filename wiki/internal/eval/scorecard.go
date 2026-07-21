package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type Scorecard struct {
	Cases         []CaseScore `json:"cases"`
	MeanComposite float64     `json:"mean_composite"`
	Config        Config      `json:"config"`
	RunComposites []float64   `json:"run_composites,omitempty"`
	Epsilon       float64     `json:"epsilon,omitempty"`
}

type fixedFloat float64

func (f fixedFloat) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%.6f", f)), nil
}

type deterministicMetrics struct {
	Matched   int        `json:"matched"`
	Missed    int        `json:"missed"`
	Spurious  int        `json:"spurious"`
	Precision fixedFloat `json:"precision"`
	Recall    fixedFloat `json:"recall"`
	F1        fixedFloat `json:"f1"`
}

type deterministicCase struct {
	Name          string               `json:"name"`
	Difficulty    string               `json:"difficulty"`
	Subjects      deterministicMetrics `json:"subjects"`
	Claims        deterministicMetrics `json:"claims"`
	FieldCorrect  int                  `json:"field_correct"`
	FieldTotal    int                  `json:"field_total"`
	FieldAccuracy fixedFloat           `json:"field_accuracy"`
	Composite     fixedFloat           `json:"composite"`
	PromptSHA256  string               `json:"prompt_sha256"`
}

type deterministicEmbedding struct {
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	Dimensions int        `json:"dimensions"`
	Threshold  fixedFloat `json:"threshold"`
	Margin     fixedFloat `json:"margin"`
}

type deterministicWeights struct {
	Subject fixedFloat `json:"subject"`
	Claim   fixedFloat `json:"claim"`
	Field   fixedFloat `json:"field"`
}

type deterministicConfig struct {
	Eval      EvalCall               `json:"eval"`
	Embedding deterministicEmbedding `json:"embedding"`
	Weights   deterministicWeights   `json:"weights"`
}

func Aggregate(cases []CaseScore, cfg Config) Scorecard {
	ordered := append([]CaseScore(nil), cases...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	var total float64
	for _, score := range ordered {
		total += score.Composite
	}
	card := Scorecard{Cases: ordered, Config: cfg}
	if len(ordered) > 0 {
		card.MeanComposite = total / float64(len(ordered))
	}
	return card
}

func (s Scorecard) MarshalDeterministic() ([]byte, error) {
	cases := append([]CaseScore(nil), s.Cases...)
	sort.SliceStable(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	var buffer bytes.Buffer
	buffer.WriteString(`{"cases":[`)
	for i, score := range cases {
		if i > 0 {
			buffer.WriteByte(',')
		}
		data, err := json.Marshal(deterministicCase{
			Name: score.Name, Difficulty: score.Difficulty,
			Subjects: deterministicMetric(score.Subjects), Claims: deterministicMetric(score.Claims),
			FieldCorrect: score.FieldCorrect, FieldTotal: score.FieldTotal,
			FieldAccuracy: fixedFloat(score.FieldAccuracy), Composite: fixedFloat(score.Composite),
			PromptSHA256: score.PromptSHA256,
		})
		if err != nil {
			return nil, err
		}
		buffer.Write(data)
	}
	buffer.WriteString(`],"mean_composite":`)
	buffer.WriteString(fmt.Sprintf("%.6f", s.MeanComposite))
	buffer.WriteString(`,"config":`)
	data, err := json.Marshal(deterministicConfig{
		Eval: s.Config.Eval,
		Embedding: deterministicEmbedding{
			Provider: s.Config.Embedding.Provider, Model: s.Config.Embedding.Model,
			Dimensions: s.Config.Embedding.Dimensions, Threshold: fixedFloat(s.Config.Embedding.Threshold), Margin: fixedFloat(s.Config.Embedding.Margin),
		},
		Weights: deterministicWeights{fixedFloat(s.Config.Weights.Subject), fixedFloat(s.Config.Weights.Claim), fixedFloat(s.Config.Weights.Field)},
	})
	if err != nil {
		return nil, err
	}
	buffer.Write(data)
	if len(s.RunComposites) > 0 {
		buffer.WriteString(`,"run_composites":[`)
		for i, composite := range s.RunComposites {
			if i > 0 {
				buffer.WriteByte(',')
			}
			buffer.WriteString(fmt.Sprintf("%.6f", composite))
		}
		buffer.WriteString(`],"epsilon":`)
		buffer.WriteString(fmt.Sprintf("%.6f", s.Epsilon))
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func deterministicMetric(metric Metrics) deterministicMetrics {
	return deterministicMetrics{
		Matched: metric.Matched, Missed: metric.Missed, Spurious: metric.Spurious,
		Precision: fixedFloat(metric.Precision), Recall: fixedFloat(metric.Recall), F1: fixedFloat(metric.F1),
	}
}
