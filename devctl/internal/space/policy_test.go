package space

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

type decodedPolicy struct {
	Version   string             `json:"Version"`
	Statement []decodedStatement `json:"Statement"`
}

type decodedStatement struct {
	Effect    string                         `json:"Effect"`
	Action    json.RawMessage                `json:"Action"`
	Resource  string                         `json:"Resource"`
	Condition map[string]map[string][]string `json:"Condition"`
}

func TestPolicyDocumentContract(t *testing.T) {
	// R-VPQM-VSH2
	accept := func(func(string, string, spaceref.Space, bool) string) {}
	accept(PolicyDocument)
	p := decodePolicy(t, PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}, false))
	if p.Version != "2012-10-17" {
		t.Fatalf("Version = %q", p.Version)
	}
}

func TestPolicyDocumentIsDeterministic(t *testing.T) {
	sp := spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}
	first := PolicyDocument("ikigenba.dev", "ZONE1", sp, true)
	if second := PolicyDocument("ikigenba.dev", "ZONE1", sp, true); first != second {
		t.Fatal("identical arguments produced different policy text")
	}
}

func TestPolicyParameterAccess(t *testing.T) {
	// R-9ANZ-12GL
	p := samplePolicy(t, false)
	var actions []string
	for _, statement := range p.Statement {
		for _, action := range policyActions(t, statement) {
			if strings.HasPrefix(action, "ssm:") {
				actions = append(actions, action)
				if statement.Effect != "Allow" || statement.Resource != "arn:aws:ssm:*:*:parameter/sbx1.ikigenba.dev/*" {
					t.Errorf("SSM statement = %#v", statement)
				}
			}
		}
	}
	if !reflect.DeepEqual(actions, []string{"ssm:GetParameter"}) || strings.Contains(PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}, false), "parameter/ikigenba/") {
		t.Fatalf("SSM actions = %v", actions)
	}
}

func TestPolicyObjectAccess(t *testing.T) {
	// R-9D3R-SLXZ
	p := samplePolicy(t, false)
	var actions []string
	for _, statement := range p.Statement {
		statementActions := policyActions(t, statement)
		for _, action := range statementActions {
			if !strings.HasPrefix(action, "s3:") {
				continue
			}
			actions = append(actions, action)
			if action == "s3:ListBucket" {
				if statement.Resource != "arn:aws:s3:::ikigenba.dev" || !reflect.DeepEqual(statement.Condition["StringLike"]["s3:prefix"], []string{"sbx1/", "sbx1/*"}) {
					t.Errorf("bucket statement = %#v", statement)
				}
			} else if statement.Resource != "arn:aws:s3:::ikigenba.dev/sbx1/*" {
				t.Errorf("object statement = %#v", statement)
			}
		}
	}
	slices.Sort(actions)
	if want := []string{"s3:GetObject", "s3:ListBucket", "s3:PutObject"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("S3 actions = %v, want %v", actions, want)
	}
	if strings.Contains(string(mustJSON(t, p)), "sbx2/") {
		t.Fatal("policy grants access to sbx2")
	}
}

func TestPolicyRoute53Access(t *testing.T) {
	// R-VS6F-NBYG
	for _, apex := range []bool{false, true} {
		p := samplePolicy(t, apex)
		var actions []string
		for _, statement := range p.Statement {
			for _, action := range policyActions(t, statement) {
				if !strings.HasPrefix(action, "route53:") {
					continue
				}
				actions = append(actions, action)
				switch action {
				case "route53:GetChange":
					if statement.Resource != "arn:aws:route53:::change/*" {
						t.Errorf("GetChange resource = %q", statement.Resource)
					}
				case "route53:ListResourceRecordSets":
					if statement.Resource != "arn:aws:route53:::hostedzone/Z09565073GHK8BYWQ1A78" {
						t.Errorf("list resource = %q", statement.Resource)
					}
				case "route53:ChangeResourceRecordSets":
					wantNames := []string{"sbx1.ikigenba.dev", "*.sbx1.ikigenba.dev"}
					if apex {
						wantNames = append(wantNames, "_acme-challenge.ikigenba.dev")
					}
					if statement.Resource != "arn:aws:route53:::hostedzone/Z09565073GHK8BYWQ1A78" ||
						!reflect.DeepEqual(statement.Condition["ForAllValues:StringEquals"]["route53:ChangeResourceRecordSetsRecordTypes"], []string{"TXT"}) ||
						!reflect.DeepEqual(statement.Condition["ForAllValues:StringLike"]["route53:ChangeResourceRecordSetsNormalizedRecordNames"], wantNames) {
						t.Errorf("change statement = %#v", statement)
					}
				}
			}
		}
		slices.Sort(actions)
		want := []string{"route53:ChangeResourceRecordSets", "route53:GetChange", "route53:ListResourceRecordSets"}
		if !reflect.DeepEqual(actions, want) {
			t.Fatalf("Route 53 actions = %v, want %v", actions, want)
		}
	}
}

func samplePolicy(t *testing.T, apex bool) decodedPolicy {
	t.Helper()
	return decodePolicy(t, PolicyDocument(
		"ikigenba.dev",
		"Z09565073GHK8BYWQ1A78",
		spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"},
		apex,
	))
}

func decodePolicy(t *testing.T, document string) decodedPolicy {
	t.Helper()
	var decoded decodedPolicy
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	return decoded
}

func policyActions(t *testing.T, statement decodedStatement) []string {
	t.Helper()
	var many []string
	if err := json.Unmarshal(statement.Action, &many); err == nil {
		return many
	}
	var one string
	if err := json.Unmarshal(statement.Action, &one); err != nil {
		t.Fatalf("parse action %s: %v", statement.Action, err)
	}
	return []string{one}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
