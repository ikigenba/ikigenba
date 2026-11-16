package tools_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-6QJY-9VA8
func TestRefusalCopyConstants(t *testing.T) {
	const (
		missing      string = tools.MissingSite
		name         string = tools.InvalidName
		ref          string = tools.InvalidRef
		path         string = tools.InvalidPath
		directory    string = tools.NoDirectory
		taken        string = tools.NameTaken
		repo         string = tools.NoRepository
		unavailable  string = tools.RepoUnavailable
		commit       string = tools.NoCommit
		domain       string = tools.InvalidDomain
		space        string = tools.SpaceDomain
		domainTaken  string = tools.DomainTaken
		domainPublic string = tools.DomainNotPublic
		timeout      string = tools.TimedOut
		size         string = tools.TooLarge
		visibility   string = tools.BadVisibility
		update       string = tools.EmptyUpdate
		both         string = tools.NameOrClear
		apex         string = tools.ApexNotPublic
		git          string = tools.GitFailed
	)
	seen := map[string]bool{}
	all := []string{missing, name, ref, path, taken, repo, unavailable, commit, directory, domain, space, domainTaken, timeout, size, visibility, update, both, apex, domainPublic, git}
	for i, c := range all {
		if c == "" || strings.Contains(c, "\n") || seen[c] {
			t.Fatalf("invalid copy constant %d", i)
		}
		seen[c] = true
		var result string
		switch {
		case i < 12:
			result = fmt.Sprintf(c, "operand-probe")
			if strings.Count(result, "operand-probe") != 1 {
				t.Fatalf("string operand in constant %d", i)
			}
		case i < 14:
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
