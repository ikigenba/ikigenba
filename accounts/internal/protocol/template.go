package protocol

import (
	"cmp"
	"slices"
)

// Team template defaults and the bounds on reported template commits.
const (
	DefaultTemplateURL = "https://github.com/ikigenba/team-template"
	DefaultTemplateRef = "main"
	MaxSeeds           = 50
	MaxHomes           = 200
)

// Seed records the template commit used to populate an account's folder.
type Seed struct {
	GID    int64  `json:"gid"`
	Commit string `json:"commit"`
}

// Home records the template commit used to populate a user's home file.
type Home struct {
	UID    int64  `json:"uid"`
	Commit string `json:"commit"`
}

// ValidTemplateCommit reports whether a commit is a lowercase SHA-1 or SHA-256 hex ID.
func ValidTemplateCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	for i := range len(commit) {
		b := commit[i]
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// boundTemplateEntries retains the first valid entry for each of the lowest IDs.
func boundTemplateEntries[T any](entries []T, limit int, fields func(T) (int64, string)) []T {
	if entries == nil {
		return nil
	}
	result := make([]T, 0, min(len(entries), limit))
	for _, entry := range entries {
		id, commit := fields(entry)
		if id < 1 || !ValidTemplateCommit(commit) {
			continue
		}
		i, found := slices.BinarySearchFunc(result, id, func(other T, target int64) int {
			otherID, _ := fields(other)
			return cmp.Compare(otherID, target)
		})
		if found || i == limit {
			continue
		}
		if len(result) == limit {
			result = result[:limit-1]
		}
		result = slices.Insert(result, i, entry)
	}
	return result
}
