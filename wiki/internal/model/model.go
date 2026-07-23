// Package model contains the dependency-neutral durable wiki record types.
package model

// Subject is a canonical entity, event, or concept in the wiki.
type Subject struct {
	ID       string
	Name     string
	NormName string
	Type     string
}

// Claim is an extracted statement about a subject.
type Claim struct {
	ID        string
	SubjectID string
	JobID     string
	Body      string
}
