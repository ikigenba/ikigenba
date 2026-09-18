package spacecreate

import (
	"encoding/json"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type policy struct {
	Version   string      `json:"Version"`
	Statement []statement `json:"Statement"`
}

type statement struct {
	Effect    string                    `json:"Effect"`
	Action    any                       `json:"Action"`
	Resource  string                    `json:"Resource"`
	Condition map[string]map[string]any `json:"Condition"`
}

func TestAssumeRolePolicy(t *testing.T) {
	// R-XQZ0-EB4M
	const want = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`
	if AssumeRolePolicy != want {
		t.Fatalf("AssumeRolePolicy = %q, want %q", AssumeRolePolicy, want)
	}
}

func TestPolicyTemplateIsEmbeddedAndUnpinned(t *testing.T) {
	// R-E2L9-88ZH
	template, err := os.ReadFile("templates/space-role-policy.json")
	if err != nil {
		t.Fatalf("read policy template: %v", err)
	}
	if PolicyTemplate != string(template) {
		t.Fatal("PolicyTemplate does not contain templates/space-role-policy.json")
	}

	// R-EKVQ-YT3W
	assertNoOpsctlVersionPin(t)
}

func TestPolicyDocumentSubstitutesOnlyPlaceholders(t *testing.T) {
	// R-VWNB-MMGU
	const (
		domain    = "alpha.example.com"
		zoneID    = "Z0123456789"
		accountID = "123456789012"
		bucket    = "example-backups"
	)
	want := strings.NewReplacer(
		"<domain>", domain,
		"<zone_id>", zoneID,
		"<account_id>", accountID,
		"<bucket>", bucket,
	).Replace(PolicyTemplate)
	got := policyDocument(domain, zoneID, accountID, bucket)
	if got != want {
		t.Fatalf("policyDocument changed text beyond substitutions\ngot:  %s\nwant: %s", got, want)
	}
	for _, placeholder := range []string{"<domain>", "<zone_id>", "<account_id>", "<bucket>"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("policyDocument left placeholder %q", placeholder)
		}
	}
}

func TestPolicyObjectAccessIsConfinedToSpace(t *testing.T) {
	// R-EJNU-L1D7
	p := parsePolicy(t)
	var s3Actions []string
	for _, stmt := range p.Statement {
		for _, action := range actions(stmt) {
			if strings.HasPrefix(action, "s3:") {
				assertAllow(t, stmt, action)
				s3Actions = append(s3Actions, action)
			}
		}
	}
	slices.Sort(s3Actions)
	wantActions := []string{"s3:GetObject", "s3:ListBucket", "s3:PutObject"}
	slices.Sort(wantActions)
	if !reflect.DeepEqual(s3Actions, wantActions) {
		t.Fatalf("S3 actions = %v, want %v", s3Actions, wantActions)
	}
	object := findStatement(t, p, "s3:GetObject")
	if got, want := actions(object), []string{"s3:GetObject", "s3:PutObject"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("object actions = %v, want %v", got, want)
	}
	if object.Resource != "arn:aws:s3:::backups/space.example.com/*" {
		t.Fatalf("object resource = %q", object.Resource)
	}
	listing := findStatement(t, p, "s3:ListBucket")
	if listing.Resource != "arn:aws:s3:::backups" {
		t.Fatalf("list resource = %q", listing.Resource)
	}
	if got, want := stringValues(t, listing.Condition["StringLike"]["s3:prefix"]), []string{"space.example.com/", "space.example.com/*"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list prefixes = %v, want %v", got, want)
	}
	if !strings.HasPrefix("space.example.com/deploy/release.tar.zst", "space.example.com/") {
		t.Fatal("deploy object is not readable through the space prefix")
	}
	if strings.HasPrefix("other.example.com/deploy/release.tar.zst", "space.example.com/") {
		t.Fatal("another space's object matched the space prefix")
	}
}

func TestPolicyParameterAccessIsReadOnlyAndConfined(t *testing.T) {
	// R-F3DQ-ZZV8
	p := parsePolicy(t)
	var ssmActions []string
	for _, stmt := range p.Statement {
		for _, action := range actions(stmt) {
			if strings.HasPrefix(action, "ssm:") {
				assertAllow(t, stmt, action)
				ssmActions = append(ssmActions, action)
				if stmt.Resource != "arn:aws:ssm:*:123456789012:parameter/ikigenba/space.example.com/*" {
					t.Fatalf("SSM resource = %q", stmt.Resource)
				}
			}
		}
	}
	if want := []string{"ssm:GetParameter"}; !reflect.DeepEqual(ssmActions, want) {
		t.Fatalf("SSM actions = %v, want %v", ssmActions, want)
	}
}

func TestPolicyRoute53AccessIsTXTOnly(t *testing.T) {
	// R-CAAK-IWJI
	p := parsePolicy(t)
	var routeActions []string
	for _, stmt := range p.Statement {
		for _, action := range actions(stmt) {
			if strings.HasPrefix(action, "route53:") {
				assertAllow(t, stmt, action)
				routeActions = append(routeActions, action)
			}
		}
	}
	slices.Sort(routeActions)
	want := []string{"route53:ChangeResourceRecordSets", "route53:GetChange", "route53:ListResourceRecordSets"}
	slices.Sort(want)
	if !reflect.DeepEqual(routeActions, want) {
		t.Fatalf("Route 53 actions = %v, want %v", routeActions, want)
	}

	change := findStatement(t, p, "route53:ChangeResourceRecordSets")
	if change.Resource != "arn:aws:route53:::hostedzone/Z0123456789" {
		t.Fatalf("change resource = %q", change.Resource)
	}
	list := findStatement(t, p, "route53:ListResourceRecordSets")
	if list.Resource != "arn:aws:route53:::hostedzone/Z0123456789" {
		t.Fatalf("list resource = %q", list.Resource)
	}
	getChange := findStatement(t, p, "route53:GetChange")
	if getChange.Resource != "arn:aws:route53:::change/*" {
		t.Fatalf("get-change resource = %q", getChange.Resource)
	}
	if got, want := stringValues(t, change.Condition["ForAllValues:StringEquals"]["route53:ChangeResourceRecordSetsRecordTypes"]), []string{"TXT"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("record types = %v, want %v", got, want)
	}
	if got, want := stringValues(t, change.Condition["ForAllValues:StringLike"]["route53:ChangeResourceRecordSetsNormalizedRecordNames"]), []string{"space.example.com", "*.space.example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("record names = %v, want %v", got, want)
	}
	if !recordChangeAllowed(change, "TXT", "_acme-challenge.space.example.com") {
		t.Fatal("ACME TXT record is not writable")
	}
	if recordChangeAllowed(change, "A", "space.example.com") || recordChangeAllowed(change, "A", "www.space.example.com") {
		t.Fatal("an A record is writable")
	}
}

func parsePolicy(t *testing.T) policy {
	t.Helper()
	var p policy
	document := policyDocument("space.example.com", "Z0123456789", "123456789012", "backups")
	if err := json.Unmarshal([]byte(document), &p); err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	return p
}

func assertNoOpsctlVersionPin(t *testing.T) {
	t.Helper()
	target := os.DirFS("../..")
	for _, tree := range []string{"cmd", "internal"} {
		err := fs.WalkDir(target, tree, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			if entry.Name() == "opsctl-version" {
				t.Errorf("current target contains opsctl version pin file %s", path)
				return nil
			}
			contents, err := fs.ReadFile(target, path)
			if err != nil {
				return err
			}
			if strings.Contains(string(contents), "opsctl-version") {
				t.Errorf("current target contains opsctl version pin in %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("search current target for opsctl version pin: %v", err)
		}
	}
}

func assertAllow(t *testing.T, stmt statement, action string) {
	t.Helper()
	if stmt.Effect != "Allow" {
		t.Errorf("%s effect = %q, want Allow", action, stmt.Effect)
	}
}

func actions(stmt statement) []string {
	switch value := stmt.Action.(type) {
	case string:
		return []string{value}
	case []any:
		result := make([]string, len(value))
		for i := range value {
			result[i], _ = value[i].(string)
		}
		return result
	default:
		return nil
	}
}

func findStatement(t *testing.T, p policy, action string) statement {
	t.Helper()
	for _, stmt := range p.Statement {
		if slices.Contains(actions(stmt), action) {
			return stmt
		}
	}
	t.Fatalf("no statement grants %s", action)
	return statement{}
}

func stringValues(t *testing.T, value any) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("condition value has type %T, want array", value)
	}
	result := make([]string, len(values))
	for i := range values {
		var itemOK bool
		result[i], itemOK = values[i].(string)
		if !itemOK {
			t.Fatalf("condition item has type %T, want string", values[i])
		}
	}
	return result
}

func recordChangeAllowed(stmt statement, recordType, name string) bool {
	types, _ := stmt.Condition["ForAllValues:StringEquals"]["route53:ChangeResourceRecordSetsRecordTypes"].([]any)
	names, _ := stmt.Condition["ForAllValues:StringLike"]["route53:ChangeResourceRecordSetsNormalizedRecordNames"].([]any)
	typeAllowed := slices.Contains(types, any(recordType))
	nameAllowed := false
	for _, pattern := range names {
		text, _ := pattern.(string)
		nameAllowed = nameAllowed || name == text || (strings.HasPrefix(text, "*.") && strings.HasSuffix(name, text[1:]))
	}
	return typeAllowed && nameAllowed
}
