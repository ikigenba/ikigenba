// Package source defines the common envelope handed from scrapers to the store.
package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// Address holds the normalized business address.
type Address struct{ Street, City, State, Zip, County string }

// Person holds a named party and its source role.
type Person struct{ Name, Role string }

// Row holds an envelope and its original source record.
type Row struct {
	Scraper, Run, SourceID string
	Scraped                time.Time
	Digest, Name           string
	Names                  []string
	Address                Address
	Lat, Lon               *float64
	People                 []Person
	Facts, Raw             json.RawMessage
}

// Scraper walks a source and hands its pages to a batch consumer.
type Scraper interface {
	Name() string
	Scrape(context.Context, func([]Row) error) error
}

// Called names the event emitted for a source request.
const Called = "source.called"

// MarshalJSON encodes the common envelope in its declared member order.
func (r Row) MarshalJSON() ([]byte, error) {
	type address struct {
		Street string `json:"street,omitempty"`
		City   string `json:"city,omitempty"`
		State  string `json:"state,omitempty"`
		Zip    string `json:"zip,omitempty"`
		County string `json:"county,omitempty"`
	}
	type person struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	names := r.Names
	if names == nil {
		names = []string{}
	}
	people := make([]person, len(r.People))
	for i, p := range r.People {
		people[i] = person(p)
	}
	facts := r.Facts
	if len(facts) == 0 {
		facts = json.RawMessage(`{}`)
	}
	return json.Marshal(struct {
		Scraper  string          `json:"scraper"`
		Run      string          `json:"run"`
		SourceID string          `json:"source_id"`
		Scraped  string          `json:"scraped"`
		Digest   string          `json:"digest"`
		Name     string          `json:"name"`
		Names    []string        `json:"names"`
		Address  address         `json:"address"`
		Lat      *float64        `json:"lat,omitempty"`
		Lon      *float64        `json:"lon,omitempty"`
		People   []person        `json:"people"`
		Facts    json.RawMessage `json:"facts"`
		Raw      json.RawMessage `json:"raw,omitempty"`
	}{r.Scraper, r.Run, r.SourceID, r.Scraped.UTC().Format(time.RFC3339), r.Digest, r.Name, names, address(r.Address), r.Lat, r.Lon, people, facts, r.Raw})
}

// Names trims and deduplicates names while preserving their order.
func Names(names ...string) []string {
	result := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result
}

// TitleCase cases letters according to the preceding rune.
func TitleCase(s string) string {
	var result strings.Builder
	previousLetter := false
	for _, r := range s {
		letter := unicode.IsLetter(r)
		if letter {
			if previousLetter {
				r = unicode.ToLower(r)
			} else {
				r = unicode.ToUpper(r)
			}
		}
		result.WriteRune(r)
		previousLetter = letter
	}
	return result.String()
}

// NewAddress normalizes the five address parts.
func NewAddress(street, city, state, zip, county string) Address {
	z := []rune(strings.TrimSpace(zip))
	if len(z) > 5 {
		z = z[:5]
	}
	return Address{strings.TrimSpace(street), TitleCase(strings.TrimSpace(city)), strings.ToUpper(strings.TrimSpace(state)), string(z), strings.TrimSpace(county)}
}

// Digest hashes the exact source bytes with SHA-256.
func Digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
