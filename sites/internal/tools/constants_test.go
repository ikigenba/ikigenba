package tools_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-ZKQP-Z7MQ
func TestRefusalCopyConstants(t *testing.T) {
	const (
		missing     string = tools.MissingSite
		name        string = tools.InvalidName
		ref         string = tools.InvalidRef
		taken       string = tools.NameTaken
		repo        string = tools.NoRepository
		unavailable string = tools.RepoUnavailable
		commit      string = tools.NoCommit
		timeout     string = tools.TimedOut
		size        string = tools.TooLarge
		visibility  string = tools.BadVisibility
		update      string = tools.EmptyUpdate
		both        string = tools.NameOrClear
		apex        string = tools.ApexNotPublic
		git         string = tools.GitFailed
	)
	seen := map[string]bool{}
	all := []string{missing, name, ref, taken, repo, unavailable, commit, timeout, size, visibility, update, both, apex, git}
	for i, c := range all {
		if c == "" || strings.Contains(c, "\n") || seen[c] {
			t.Fatalf("invalid copy constant %d", i)
		}
		seen[c] = true
		var result string
		switch {
		case i < 7:
			result = fmt.Sprintf(c, "operand-probe")
			if strings.Count(result, "operand-probe") != 1 {
				t.Fatalf("string operand in constant %d", i)
			}
		case i < 9:
			result = fmt.Sprintf(c, 918273)
			if result == fmt.Sprintf(c, 0) {
				t.Fatalf("integer operand not used in constant %d", i)
			}
		default:
			result = c
		}
		if strings.Contains(result, "%!") {
			t.Fatalf("invalid format constant %d", i)
		}
	}
}
