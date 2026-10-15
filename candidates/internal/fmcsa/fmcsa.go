// Package fmcsa interprets the FMCSA scraper's rows.
package fmcsa

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Name is the scraper this source interprets.
const Name = "fmcsa"

// Source applies versioned carrier rules to one row.
type Source struct{}

// New constructs the stateless FMCSA source.
func New() *Source { return &Source{} }

// Name returns the scraper name.
func (*Source) Name() string { return Name }
func stringFact(f map[string]json.RawMessage, k string) string {
	var s string
	if json.Unmarshal(f[k], &s) != nil {
		return ""
	}
	return s
}
func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func count(f map[string]json.RawMessage, key string) (int64, bool) {
	s := stringFact(f, key)
	n, err := strconv.ParseInt(s, 10, 64)
	return n, digits(s) && err == nil && n <= 36893488147419
}

// Read returns an independent interpretation of r.
func (*Source) Read(r scrapes.Row) sources.Reading {
	out := sources.Reading{NameKey: sources.NameKey(r.Name), Name: r.Name, Names: append([]string{}, r.Names...), Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, County: sources.County(r.Address.State, r.Address.County), Phone: stringFact(r.Facts, "phone"), Email: stringFact(r.Facts, "email"), People: make([]sources.Person, 0, len(r.People)), Class: sources.ClassUnknown, Cargo: []string{}, Exclusions: []sources.Exclusion{}}
	out.PhoneKey = sources.PhoneKey(out.Phone)
	out.DomainKey = sources.DomainKey(out.Email)
	for _, p := range r.People {
		out.People = append(out.People, sources.Person{Name: p.Name, Role: p.Role})
	}
	parts := make(map[string]bool)
	for _, p := range strings.Split(stringFact(r.Facts, "class"), ";") {
		parts[strings.ToUpper(strings.TrimSpace(p))] = true
	}
	hire, private := parts["AUTHORIZED FOR HIRE"] || parts["EXEMPT FOR HIRE"], parts["PRIVATE PROPERTY"]
	switch {
	case hire && private:
		out.Class = sources.ClassMixed
	case hire:
		out.Class = sources.ClassForHire
	case private:
		out.Class = sources.ClassPrivate
	}
	var cargo []json.RawMessage
	if json.Unmarshal(r.Facts["cargo"], &cargo) == nil {
		for _, v := range cargo {
			var s string
			if len(strings.TrimSpace(string(v))) > 0 && strings.TrimSpace(string(v))[0] == '"' && json.Unmarshal(v, &s) == nil {
				out.Cargo = append(out.Cargo, s)
			}
		}
	}
	date := stringFact(r.Facts, "mcs150_date")
	if len(date) == 8 && digits(date) {
		if t, err := time.Parse("20060102", date); err == nil {
			out.Filed = t
		}
	}
	n, unitsRead := count(r.Facts, "power_units")
	drivers, driversRead := count(r.Facts, "total_drivers")
	mileage, mileageRead := count(r.Facts, "mileage")
	driverFloor := max(drivers, 1)
	implausible := unitsRead && driversRead && n > 50*driverFloor && (!mileageRead || mileage == 0)
	suspicious := !implausible && unitsRead && driversRead && n > 10*driverFloor
	if unitsRead && !implausible {
		out.Fleet = stringFact(r.Facts, "status") == "A" && n >= 2
		est := &sources.Estimate{Source: Name, Low: 150000 * n, Facts: make(map[string]json.RawMessage), Filed: out.Filed, Formula: sources.Rules}
		switch out.Class {
		case sources.ClassForHire:
			h := 250000 * n
			est.High = &h
			est.Confidence = 0.7
		case sources.ClassPrivate:
			est.Confidence = 0.3
		case sources.ClassMixed:
			est.Confidence = 0.5
		default:
			est.Confidence = 0.2
		}
		if suspicious {
			est.Confidence /= 2
			est.Facts["plausibility"] = json.RawMessage(`"suspicious"`)
		}
		for _, k := range []string{"power_units", "class", "mcs150_date", "total_drivers", "mileage"} {
			if v, ok := r.Facts[k]; ok {
				est.Facts[k] = append(json.RawMessage(nil), v...)
			}
		}
		out.Estimate = est
	}
	for _, p := range []string{"STATE GOVERNMENT", "LOCAL GOVERNMENT", "FEDERAL GOVERNMENT", "INDIAN NATION"} {
		if parts[p] {
			out.Exclusions = append(out.Exclusions, sources.Exclusion{Reason: sources.ExcludeGovernmentClass, Detail: p})
		}
	}
	for _, p := range []string{"COUNTY", "CITY OF", "TOWN OF", "VILLAGE OF", "TOWNSHIP", "SCHOOL DISTRICT", "STATE OF", "DEPT", "DEPARTMENT", "UNIVERSITY", "PUBLIC WORKS"} {
		if strings.Contains(" "+out.NameKey+" ", " "+p+" ") {
			out.Exclusions = append(out.Exclusions, sources.Exclusion{Reason: sources.ExcludeGovernmentName, Detail: p})
		}
	}
	return out
}
