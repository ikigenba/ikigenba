package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"wiki/internal/extract"
	"wiki/internal/wiki"
)

type AnalysisGoldCase struct {
	Name       string
	Difficulty string
	Question   string
	Gold       wiki.QueryAnalysis
}

func LoadAnalysisGold(root string) (dev, holdout []AnalysisGoldCase, err error) {
	dev, err = loadAnalysisSplit(root, "dev")
	if err != nil {
		return nil, nil, err
	}
	holdout, err = loadAnalysisSplit(root, "holdout")
	if err != nil {
		return nil, nil, err
	}
	return dev, holdout, nil
}

func loadAnalysisSplit(root, split string) ([]AnalysisGoldCase, error) {
	entries, err := os.ReadDir(filepath.Join(root, split))
	if err != nil {
		return nil, fmt.Errorf("read analysis gold %s: %w", split, err)
	}
	var cases []AnalysisGoldCase
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		dir := filepath.Join(root, split, name)
		question, err := os.ReadFile(filepath.Join(dir, "question.txt"))
		if err != nil {
			return nil, fmt.Errorf("analysis gold case %s question.txt: %w", name, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "gold.json"))
		if err != nil {
			return nil, fmt.Errorf("analysis gold case %s gold.json: %w", name, err)
		}
		var disk struct {
			Difficulty string              `json:"difficulty"`
			Gold       *wiki.QueryAnalysis `json:"gold"`
		}
		if err := json.Unmarshal(data, &disk); err != nil {
			return nil, fmt.Errorf("analysis gold case %s malformed gold.json: %w", name, err)
		}
		if disk.Gold == nil {
			return nil, fmt.Errorf("analysis gold case %s missing gold object", name)
		}
		cases = append(cases, AnalysisGoldCase{Name: name, Difficulty: disk.Difficulty, Question: string(question), Gold: *disk.Gold})
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

type GoldCase struct {
	Name       string
	Difficulty string
	Header     extract.DocumentHeader
	Document   string
	Gold       []GoldSubject
}

type GoldSubject struct {
	Type       string   `json:"type"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
	OccurredAt string   `json:"occurred_at"`
	Claims     []string `json:"claims"`
}

func LoadGold(root string) (dev, holdout []GoldCase, err error) {
	dev, err = loadSplit(root, "dev")
	if err != nil {
		return nil, nil, err
	}
	holdout, err = loadSplit(root, "holdout")
	if err != nil {
		return nil, nil, err
	}
	return dev, holdout, nil
}

func loadSplit(root, split string) ([]GoldCase, error) {
	entries, err := os.ReadDir(filepath.Join(root, split))
	if err != nil {
		return nil, fmt.Errorf("read gold %s: %w", split, err)
	}
	var cases []GoldCase
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		c, err := loadCase(filepath.Join(root, split, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

func loadCase(dir, name string) (GoldCase, error) {
	document, err := os.ReadFile(filepath.Join(dir, "document.txt"))
	if err != nil {
		return GoldCase{}, fmt.Errorf("gold case %s document.txt: %w", name, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gold.json"))
	if err != nil {
		return GoldCase{}, fmt.Errorf("gold case %s gold.json: %w", name, err)
	}
	var disk struct {
		Difficulty string `json:"difficulty"`
		Header     struct {
			Source     string   `json:"source"`
			Title      string   `json:"title"`
			Tags       []string `json:"tags"`
			ReceivedAt string   `json:"received_at"`
		} `json:"header"`
		Gold []GoldSubject `json:"gold"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return GoldCase{}, fmt.Errorf("gold case %s malformed gold.json: %w", name, err)
	}
	receivedAt, err := time.Parse(time.RFC3339, disk.Header.ReceivedAt)
	if err != nil {
		return GoldCase{}, fmt.Errorf("gold case %s header.received_at: %w", name, err)
	}
	for i, subject := range disk.Gold {
		if subject.Type != "entity" && subject.Type != "event" && subject.Type != "concept" {
			return GoldCase{}, fmt.Errorf("gold case %s subject %d has unknown type %q", name, i, subject.Type)
		}
		if subject.Name == "" {
			return GoldCase{}, fmt.Errorf("gold case %s subject %d has empty name", name, i)
		}
		if len(subject.Claims) == 0 {
			return GoldCase{}, fmt.Errorf("gold case %s subject %q has zero claims", name, subject.Name)
		}
	}
	return GoldCase{
		Name:       name,
		Difficulty: disk.Difficulty,
		Header: extract.DocumentHeader{
			Source: disk.Header.Source, Title: disk.Header.Title, Tags: disk.Header.Tags, ReceivedAt: receivedAt,
		},
		Document: string(document), Gold: disk.Gold,
	}, nil
}
