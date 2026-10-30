package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
)

func TestAccountVocabulary(t *testing.T) {
	// R-00ZU-A64U R-027Q-NXVJ R-03FN-1PM8 R-04NJ-FHCX R-05VF-T93M
	reason := "literal\nreason"
	var err error = &accounts.RefusedError{Reason: reason}
	if err.Error() != reason {
		t.Fatal(err)
	}
	if accounts.SrvDir != "/srv" {
		t.Fatal(accounts.SrvDir)
	}
	for _, name := range []string{"a", "acme", "_x", strings.Repeat("a", 32), "a-0_b"} {
		if !accounts.ValidName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 33), "Acme", "1acme", "-acme", "acme/corp", "acme$", "acme.corp", "a\n", "aé", "a\x00"} {
		if accounts.ValidName(name) {
			t.Errorf("accepted %q", name)
		}
	}
	// R-1HFI-QB0P R-1INF-42RE R-1JVB-HUI3
	for _, tc := range []struct {
		value any
		want  string
	}{
		{accounts.GroupCount{Slug: "acme", Members: 2}, `{"slug":"acme","members":2}`},
		{accounts.GroupList{Groups: []accounts.GroupCount{}}, `{"groups":[]}`},
		{accounts.MemberList{Group: "acme", Members: []string{"bob"}}, `{"group":"acme","members":["bob"]}`},
	} {
		data, err := json.Marshal(tc.value)
		if err != nil || string(data) != tc.want {
			t.Errorf("JSON %s, %v", data, err)
		}
	}
}

func TestLocalVocabularyAndMemberQueries(t *testing.T) {
	// R-09J4-YKBP R-UNIU-EKTL R-0EEQ-HNAH R-0FMM-VF16 R-0GUJ-96RV R-10CX-DIMZ R-11KT-RADO
	f := newGroupFixture(t, "\nglobex:x:80:80::/srv/globex:/no\nacme:x:81:81::/srv/acme:/no\nalice:x:90:90::/home/alice:/bin/bash\nbob:x:91:91::/home/bob:/bin/bash\nec2-user:x:92:92::/home/ec2-user:/bin/bash\nlonely:x:93:93::/srv/lonely:/no\nwrong:x:94:94::/home/wrong:/no\n", "\nacme:x:81:bob,alice,bob,ec2-user,missing,acme\nglobex:x:80:\nshell:x:70:bob,alice,missing\nwheel:x:71:alice\nwrong:x:94:alice\n")
	before := groupTreeState(t, f.root)
	got, err := accounts.ListGroups(context.Background(), f.env())
	want := accounts.GroupList{Groups: []accounts.GroupCount{{Slug: "acme", Members: 2}, {Slug: "globex", Members: 0}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %+v, %v", got, err)
	}
	for _, slug := range []string{"acme", "globex"} {
		got, err := accounts.GroupMembers(context.Background(), f.env(), slug)
		want := []string{}
		if slug == "acme" {
			want = []string{"alice", "bob"}
		}
		if err != nil || got.Group != slug || !reflect.DeepEqual(got.Members, want) {
			t.Errorf("members %+v, %v", got, err)
		}
	}
	if len(f.commands) != 0 {
		t.Fatal(f.commands)
	}
	if !reflect.DeepEqual(before, groupTreeState(t, f.root)) {
		t.Fatal("query changed files")
	}
	empty := newGroupFixture(t, "", "shell:x:70:\n")
	list, err := accounts.ListGroups(context.Background(), empty.env())
	if err != nil || list.Groups == nil || len(list.Groups) != 0 {
		t.Fatalf("empty %+v, %v", list, err)
	}
}

func TestMalformedAccountFilesAreOperationalErrors(t *testing.T) {
	// R-0BYX-Q3T3
	for _, file := range []string{"/etc/passwd", "/etc/group"} {
		for _, state := range []string{"missing", "malformed", "unreadable"} {
			for _, action := range []string{"add", "remove", "members", "list"} {
				t.Run(file+state+action, func(t *testing.T) {
					f := newGroupFixture(t, "acme:x:80:80::/srv/acme:/no\n", "acme:x:80:\n")
					if err := os.Remove(f.path(file)); err != nil {
						t.Fatal(err)
					}
					switch state {
					case "malformed":
						f.write(file, "bad:fields\n")
					case "unreadable":
						if err := os.Mkdir(f.path(file), 0700); err != nil {
							t.Fatal(err)
						}
					}
					before := groupTreeState(t, f.root)
					err := callGroupOperation(context.Background(), f.env(), action, "acme")
					if !reflect.DeepEqual(before, groupTreeState(t, f.root)) {
						t.Fatal("malformed account file changed files")
					}
					var refused *accounts.RefusedError
					if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
						t.Fatalf("error %v, commands %v", err, f.commands)
					}
					if _, err := os.Stat(f.path("/srv")); !os.IsNotExist(err) {
						t.Fatal("filesystem changed", err)
					}
				})
			}
		}
	}
}

func TestInvalidGroupNames(t *testing.T) {
	// R-0JAC-0Q99
	for _, action := range []string{"add", "remove", "members"} {
		f := newGroupFixture(t, "", "")
		before := groupTreeState(t, f.root)
		err := callGroupOperation(context.Background(), f.env(), action, "bad/name")
		if !reflect.DeepEqual(before, groupTreeState(t, f.root)) {
			t.Fatal("invalid name changed files")
		}
		var refused *accounts.RefusedError
		if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
			t.Fatalf("%s: %v, %v", action, err, f.commands)
		}
		if _, err := os.Stat(f.path("/srv")); !os.IsNotExist(err) {
			t.Fatal("filesystem changed", err)
		}
	}
}
