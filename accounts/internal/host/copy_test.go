package host_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
)

// R-D2OP-A328
func TestRefusalCopy(t *testing.T) {
	const (
		invalidName   string = host.RefuseInvalidName
		invalidKey    string = host.RefuseInvalidKey
		noGroup       string = host.RefuseNoGroup
		noUser        string = host.RefuseNoUser
		nameTaken     string = host.RefuseNameTaken
		groupNotEmpty string = host.RefuseGroupNotEmpty
		userNotLocked string = host.RefuseUserNotLocked
	)
	seen := map[string]bool{}
	for _, diagnostic := range []string{invalidName, invalidKey, noGroup, noUser, nameTaken, groupNotEmpty, userNotLocked} {
		if diagnostic == "" || strings.Contains(diagnostic, "\n") || seen[diagnostic] {
			t.Fatalf("invalid or repeated refusal copy %q", diagnostic)
		}
		seen[diagnostic] = true
	}
	m := host.NewMemory()
	requireOK(t, m.GroupAdd(background, "business"))
	requireOK(t, m.UserAdd(background, "person", "business", publicKey(1)))
	for _, tc := range []struct {
		diagnostic string
		err        error
	}{
		{invalidName, m.GroupAdd(background, "Invalid")},
		{invalidKey, m.UserKeyAdd(background, "person", "invalid")},
		{noGroup, m.GroupRemove(background, "absent")},
		{noUser, m.UserLock(background, "absent")},
		{nameTaken, m.GroupAdd(background, "person")},
		{groupNotEmpty, m.GroupRemove(background, "business")},
		{userNotLocked, m.UserDelete(background, "person")},
	} {
		var refused *host.RefusedError
		if !errors.As(tc.err, &refused) || refused.Diagnostic != tc.diagnostic {
			t.Fatalf("refusal = %v, want %q", tc.err, tc.diagnostic)
		}
	}
}

// R-D54I-1MJM
func TestHostFailedCopy(t *testing.T) {
	const failed string = host.HostFailed
	if failed == "" || strings.Contains(failed, "\n") {
		t.Fatalf("invalid HostFailed %q", failed)
	}
	s := makeStub(t, "exit 1")
	err := host.NewCommand([]string{s.path}).GroupAdd(background, "business")
	if err == nil || err.Error() != failed+" (1)" {
		t.Fatalf("command failure = %v, want HostFailed with status", err)
	}
}
