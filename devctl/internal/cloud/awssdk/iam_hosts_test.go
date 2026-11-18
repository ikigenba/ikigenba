package awssdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

const (
	operatorPolicyName = "ikigenba.dev-hosts-operator"
	operatorPolicyARN  = "arn:aws:iam::123456789012:policy/ikigenba/" + operatorPolicyName
	operatorTagKey     = "HostsOperator"
)

type iamReply struct {
	operation string
	params    map[string]string
	result    string
	code      string
	unordered bool
}

type iamTransport struct {
	t       *testing.T
	replies []iamReply
	calls   int
	mu      sync.Mutex
}

func (transport *iamTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t := transport.t
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.calls >= len(transport.replies) {
		t.Fatalf("unexpected IAM request after %d requests", transport.calls)
	}
	call := transport.calls + 1
	if request.Method != http.MethodPost || request.URL.Hostname() != "iam.amazonaws.com" || request.URL.Path != "/" {
		t.Fatalf("request %d = %s %s; want IAM query", call, request.Method, request.URL)
	}
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	form := request.PostForm
	action := form.Get("Action")
	form.Del("Action")
	form.Del("Version")
	got := make(map[string]string, len(form))
	for key, values := range form {
		if len(values) != 1 {
			t.Fatalf("request %d %s = %q, want one value", call, key, values)
		}
		got[key] = values[0]
	}
	index := transport.replyIndex(action, got)
	if index < 0 {
		t.Fatalf("request %d = %s %v, want a remaining request from %v", call, action, got, transport.replies[transport.calls:])
	}
	reply := transport.replies[index]
	transport.replies[index] = transport.replies[transport.calls]
	transport.calls++

	status := http.StatusOK
	body := fmt.Sprintf(`<%[1]sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><%[1]sResult>%[2]s</%[1]sResult></%[1]sResponse>`, reply.operation, reply.result)
	if reply.code != "" {
		status = http.StatusBadRequest
		body = fmt.Sprintf(`<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><Error><Code>%s</Code><Message>request failed</Message></Error></ErrorResponse>`, reply.code)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (transport *iamTransport) replyIndex(action string, params map[string]string) int {
	// Consecutive unordered replies may arrive in any order before the next fixed reply.
	unordered := transport.replies[transport.calls].unordered
	for i := transport.calls; i < len(transport.replies); i++ {
		reply := transport.replies[i]
		if i != transport.calls && (!unordered || !reply.unordered) {
			break
		}
		if reply.operation == action && maps.Equal(reply.params, params) {
			return i
		}
	}
	return -1
}

func scriptedIAM(t *testing.T, replies ...iamReply) cloud.IAM {
	t.Helper()
	transport := &iamTransport{t: t, replies: slices.Clone(replies)}
	t.Cleanup(func() {
		transport.mu.Lock()
		defer transport.mu.Unlock()
		if transport.calls != len(replies) {
			t.Errorf("IAM requests = %d, want %d", transport.calls, len(replies))
		}
	})
	clients, err := OpenWithLoader(t.Context(), "profile", "us-test-1", func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{
			Region:           "us-test-1",
			RetryMaxAttempts: 1,
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "fake", SecretAccessKey: "fake"}, nil
			}),
			HTTPClient: &http.Client{Transport: transport},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return clients.IAM
}

func assertIAMError(t *testing.T, err error, operation, code string) {
	t.Helper()
	var got *cloud.Error
	want := fmt.Sprintf("iam %s: %s", operation, code)
	if !errors.As(err, &got) || got.Service != "iam" || got.Operation != operation ||
		got.Subject != "" || got.Code != code || got.Error() != want {
		t.Fatalf("error = %v (%#v), want %q with matching cloud.Error fields", err, got, want)
	}
	assertUnwrappedAPIError(t, got.Err, code, "request failed")
}

func TestIAMManagedPolicy(t *testing.T) {
	// R-BM0R-25MA R-BKSU-ODVL R-BFX9-5AWT R-BH55-J2NI
	params := map[string]string{"Scope": "Local"}
	t.Run("missing", func(t *testing.T) {
		client := scriptedIAM(t, iamReply{operation: "ListPolicies", params: params, result: managedPolicyRow("another-policy")})
		arn, err := client.ManagedPolicy(t.Context(), operatorPolicyName)
		var missing *cloud.NotFoundError
		if arn != "" || !errors.As(err, &missing) || missing.Kind != "managed policy" ||
			missing.Name != operatorPolicyName || missing.Error() != "no managed policy 'ikigenba.dev-hosts-operator'" {
			t.Fatalf("ManagedPolicy = %q, %v, want managed policy not found", arn, err)
		}
	})
	t.Run("error", func(t *testing.T) {
		client := scriptedIAM(t, iamReply{operation: "ListPolicies", params: params, code: "AccessDenied"})
		_, err := client.ManagedPolicy(t.Context(), operatorPolicyName)
		assertIAMError(t, err, "ListPolicies", "AccessDenied")
	})
}

func TestIAMAttachedPolicyAndRoleTag(t *testing.T) {
	// R-C5RC-U6A5 R-C875-LPRJ R-BKSU-ODVL R-BFX9-5AWT R-BH55-J2NI
	const role = "role"
	ctx := t.Context()
	params := map[string]string{"RoleName": role}
	for _, lookup := range []struct {
		operation string
		match     string
		miss      string
		value     string
		call      func(cloud.IAM) (string, bool, error)
	}{
		{"ListAttachedRolePolicies", attachedPolicyRow(operatorPolicyName), attachedPolicyRow("another-policy"), operatorPolicyARN,
			func(client cloud.IAM) (string, bool, error) {
				return client.AttachedPolicy(ctx, role, operatorPolicyName)
			}},
		{"ListRoleTags", roleTagRow(operatorTagKey, ""), roleTagRow("AnotherTag", "true"), "",
			func(client cloud.IAM) (string, bool, error) { return client.RoleTag(ctx, role, operatorTagKey) }},
	} {
		for _, test := range []struct {
			name   string
			result string
			code   string
			found  bool
		}{
			{name: "match", result: lookup.match, found: true},
			{name: "different name", result: lookup.miss},
			{name: "role missing", code: "NoSuchEntity"},
			{name: "access denied", code: "AccessDenied"},
		} {
			t.Run(lookup.operation+"/"+test.name, func(t *testing.T) {
				client := scriptedIAM(t, iamReply{operation: lookup.operation, params: params, result: test.result, code: test.code})
				value, found, err := lookup.call(client)
				switch {
				case test.code == "AccessDenied":
					assertIAMError(t, err, lookup.operation, test.code)
				case test.found:
					if err != nil || !found || value != lookup.value {
						t.Fatalf("lookup = %q, %t, %v, want %q, true, nil", value, found, err, lookup.value)
					}
				default:
					if err != nil || found || value != "" {
						t.Fatalf("lookup = %q, %t, %v, want empty, false, nil", value, found, err)
					}
				}
			})
		}
	}
}

func TestIAMHostsRoleWrites(t *testing.T) {
	// R-BKSU-ODVL R-BFX9-5AWT R-BH55-J2NI
	const role = "sbx1.ikigenba.dev"
	ctx := t.Context()
	policy := map[string]string{"RoleName": role, "PolicyArn": operatorPolicyARN}
	tag := map[string]string{"RoleName": role, "Tags.member.1.Key": operatorTagKey, "Tags.member.1.Value": "true"}
	untag := map[string]string{"RoleName": role, "TagKeys.member.1": operatorTagKey}
	for _, test := range []struct {
		operation string
		params    map[string]string
		code      string
		call      func(cloud.IAM) error
	}{
		{"AttachRolePolicy", policy, "Throttling", func(client cloud.IAM) error { return client.AttachRolePolicy(ctx, role, operatorPolicyARN) }},
		{"DetachRolePolicy", policy, "AccessDenied", func(client cloud.IAM) error { return client.DetachRolePolicy(ctx, role, operatorPolicyARN) }},
		{"TagRole", tag, "AccessDenied", func(client cloud.IAM) error { return client.TagRole(ctx, role, operatorTagKey, "true") }},
		{"UntagRole", untag, "AccessDenied", func(client cloud.IAM) error { return client.UntagRole(ctx, role, operatorTagKey) }},
	} {
		t.Run(test.operation, func(t *testing.T) {
			client := scriptedIAM(t, iamReply{operation: test.operation, params: test.params})
			if err := test.call(client); err != nil {
				t.Fatal(err)
			}
		})
		t.Run(test.operation+" error", func(t *testing.T) {
			client := scriptedIAM(t, iamReply{operation: test.operation, params: test.params, code: test.code})
			assertIAMError(t, test.call(client), test.operation, test.code)
		})
	}
}

func TestIAMItemLookupsFollowMarkersAndStopAtMatch(t *testing.T) {
	// R-CAMY-D98X R-BM0R-25MA
	ctx := t.Context()
	for _, lookup := range []struct {
		operation string
		paramName string
		param     string
		match     string
		miss      string
		want      string
		call      func(cloud.IAM) (string, bool, error)
	}{
		{"ListPolicies", "Scope", "Local", managedPolicyRow(operatorPolicyName), managedPolicyRow("other"), operatorPolicyARN,
			func(client cloud.IAM) (string, bool, error) {
				arn, err := client.ManagedPolicy(ctx, operatorPolicyName)
				return arn, err == nil, err
			}},
		{"ListAttachedRolePolicies", "RoleName", "role", attachedPolicyRow(operatorPolicyName), attachedPolicyRow("other"), operatorPolicyARN,
			func(client cloud.IAM) (string, bool, error) {
				return client.AttachedPolicy(ctx, "role", operatorPolicyName)
			}},
		{"ListRoleTags", "RoleName", "role", roleTagRow(operatorTagKey, "true"), roleTagRow("other", "true"), "true",
			func(client cloud.IAM) (string, bool, error) { return client.RoleTag(ctx, "role", operatorTagKey) }},
	} {
		first := map[string]string{lookup.paramName: lookup.param}
		t.Run(lookup.operation+"/last page matches", func(t *testing.T) {
			client := scriptedIAM(t,
				iamReply{operation: lookup.operation, params: first, result: truncatedIAMPage(lookup.miss)},
				iamReply{operation: lookup.operation, params: map[string]string{lookup.paramName: lookup.param, "Marker": "next"}, result: lookup.match},
			)
			assertIAMLookup(t, client, lookup.want, lookup.call)
		})
		t.Run(lookup.operation+"/first page matches", func(t *testing.T) {
			client := scriptedIAM(t, iamReply{operation: lookup.operation, params: first, result: truncatedIAMPage(lookup.match)})
			assertIAMLookup(t, client, lookup.want, lookup.call)
		})
	}
}

func assertIAMLookup(t *testing.T, client cloud.IAM, want string, lookup func(cloud.IAM) (string, bool, error)) {
	t.Helper()
	value, found, err := lookup(client)
	if err != nil || !found || value != want {
		t.Fatalf("lookup = %q, %t, %v, want %q, true, nil", value, found, err, want)
	}
}

func TestIAMRolesTaggedProcessesEachPageInOrder(t *testing.T) {
	// R-C9F1-ZHI8 R-CAMY-D98X R-BTC5-CS2G R-BKSU-ODVL R-BH55-J2NI
	const (
		matchFirst  = "z-sbx1"
		matchSecond = "a-sbx2"
		mismatch    = "not-operator"
		wrongKey    = "other-tag"
		gone        = "deleted"
	)
	client := scriptedIAM(t,
		iamReply{operation: "ListRoles", result: truncatedIAMPage(rolesPage(matchFirst, mismatch, wrongKey, gone))},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": matchFirst}, result: truncatedIAMPage(roleTagRow(operatorTagKey, "true")), unordered: true},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": matchFirst, "Marker": "next"}, result: roleTagRow("other", "true"), unordered: true},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": mismatch}, result: roleTagRow(operatorTagKey, "false"), unordered: true},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": wrongKey}, result: roleTagRow("other", "true"), unordered: true},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": gone}, code: "NoSuchEntity", unordered: true},
		iamReply{operation: "ListRoles", params: map[string]string{"Marker": "next"}, result: rolesPage(matchSecond)},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": matchSecond}, result: truncatedIAMPage(roleTagRow("other", "true"))},
		iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": matchSecond, "Marker": "next"}, result: roleTagRow(operatorTagKey, "true")},
	)
	names, err := client.RolesTagged(t.Context(), operatorTagKey, "true")
	want := []string{matchFirst, matchSecond}
	if err != nil || !slices.Equal(names, want) {
		t.Fatalf("RolesTagged = %v, %v, want %v, nil", names, err, want)
	}
}

func TestIAMRolesTaggedEmptyAndErrors(t *testing.T) {
	// R-C9F1-ZHI8 R-BFX9-5AWT R-BH55-J2NI
	const role = "role"
	first := iamReply{operation: "ListRoles", result: rolesPage(role)}
	tagParams := map[string]string{"RoleName": role}
	t.Run("role removed partway through its tags", func(t *testing.T) {
		client := scriptedIAM(t, first,
			iamReply{operation: "ListRoleTags", params: tagParams, result: truncatedIAMPage(roleTagRow(operatorTagKey, "true"))},
			iamReply{operation: "ListRoleTags", params: map[string]string{"RoleName": role, "Marker": "next"}, code: "NoSuchEntity"},
		)
		names, err := client.RolesTagged(t.Context(), operatorTagKey, "true")
		if err != nil || len(names) != 0 {
			t.Fatalf("RolesTagged = %v, %v, want empty, nil", names, err)
		}
	})
	t.Run("list roles error", func(t *testing.T) {
		client := scriptedIAM(t, iamReply{operation: "ListRoles", code: "AccessDenied"})
		_, err := client.RolesTagged(t.Context(), operatorTagKey, "true")
		assertIAMError(t, err, "ListRoles", "AccessDenied")
	})
	t.Run("list tags error", func(t *testing.T) {
		client := scriptedIAM(t, first, iamReply{operation: "ListRoleTags", params: tagParams, code: "AccessDenied"})
		_, err := client.RolesTagged(t.Context(), operatorTagKey, "true")
		assertIAMError(t, err, "ListRoleTags", "AccessDenied")
	})
}

func managedPolicyRow(name string) string {
	arn := strings.TrimSuffix(operatorPolicyARN, operatorPolicyName) + name
	return "<Policies><member><PolicyName>" + name + "</PolicyName><Arn>" + arn + "</Arn></member></Policies>"
}

func attachedPolicyRow(name string) string {
	arn := strings.TrimSuffix(operatorPolicyARN, operatorPolicyName) + name
	return "<AttachedPolicies><member><PolicyName>" + name + "</PolicyName><PolicyArn>" + arn + "</PolicyArn></member></AttachedPolicies>"
}

func roleTagRow(key, value string) string {
	return "<Tags><member><Key>" + key + "</Key><Value>" + value + "</Value></member></Tags>"
}

func rolesPage(names ...string) string {
	var result strings.Builder
	result.WriteString("<Roles>")
	for _, name := range names {
		result.WriteString("<member><RoleName>" + name + "</RoleName></member>")
	}
	result.WriteString("</Roles>")
	return result.String()
}

func truncatedIAMPage(result string) string {
	return result + "<IsTruncated>true</IsTruncated><Marker>next</Marker>"
}
