package secrets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type listSSM struct {
	listedPrefix string
	parameters   []cloud.Parameter
	listErr      error
	gotName      string
	value        string
	getErr       error
}

func (ssm *listSSM) GetParameter(_ context.Context, name string) (string, error) {
	ssm.gotName = name
	return ssm.value, ssm.getErr
}

func (*listSSM) PutSecureParameter(context.Context, string, string) error { return nil }

func (ssm *listSSM) ListParameters(_ context.Context, prefix string) ([]cloud.Parameter, error) {
	ssm.listedPrefix = prefix
	return ssm.parameters, ssm.listErr
}

func (*listSSM) DeleteParameter(context.Context, string) error { return nil }

func listAccount(ssm cloud.SSM) *account.Account {
	return &account.Account{Clients: cloud.Clients{SSM: ssm}}
}

// R-GM65-DAC7
func TestListSelectsDirectParametersAndSortsEntriesAndKeys(t *testing.T) {
	t.Parallel()

	const domain = "foo.sbx.ikigenba.dev"
	prefix := Prefix(domain)
	ssm := &listSSM{parameters: []cloud.Parameter{
		{Name: prefix + "/gmail", Value: `{"ZED":"z","ALPHA":"a"}`},
		{Name: prefix, Value: `{"IGNORED":"value"}`},
		{Name: prefix + "/nested/app", Value: `{"IGNORED":"value"}`},
		{Name: prefix + "/", Value: `{"IGNORED":"value"}`},
		{Name: prefix + "-other/crm", Value: `{"IGNORED":"value"}`},
		{Name: prefix + "/dashboard", Value: `{}`},
		{Name: prefix + "/crm", Value: `{"CRM_ORG":"org","CRM_API_KEY":"key"}`},
	}}

	got, err := List(context.Background(), listAccount(ssm), domain)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	want := []Entry{
		{App: "crm", Keys: []string{"CRM_API_KEY", "CRM_ORG"}},
		{App: "dashboard", Keys: []string{}},
		{App: "gmail", Keys: []string{"ALPHA", "ZED"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List entries = %#v, want %#v", got, want)
	}
	if ssm.listedPrefix != prefix {
		t.Fatalf("ListParameters prefix = %q, want %q", ssm.listedPrefix, prefix)
	}
}

// R-GM65-DAC7
func TestListReturnsEmptyWhenNoDirectParameterExists(t *testing.T) {
	t.Parallel()

	domain := "foo.sbx.ikigenba.dev"
	ssm := &listSSM{parameters: []cloud.Parameter{{
		Name:  Prefix(domain) + "/nested/app",
		Value: `{"KEY":"value"}`,
	}}}
	got, err := List(context.Background(), listAccount(ssm), domain)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List entries = %#v, want none", got)
	}
}

// R-GNE1-R22W
func TestNamesReadsExactParameterAndSortsKeys(t *testing.T) {
	t.Parallel()

	ssm := &listSSM{value: `{"ZED":"z","ALPHA":"a"}`}
	got, err := Names(context.Background(), listAccount(ssm), "foo.sbx.ikigenba.dev", "crm")
	if err != nil {
		t.Fatalf("Names returned error: %v", err)
	}
	wantName := "/ikigenba/foo.sbx.ikigenba.dev/crm"
	if ssm.gotName != wantName {
		t.Fatalf("GetParameter name = %q, want %q", ssm.gotName, wantName)
	}
	if want := []string{"ALPHA", "ZED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %#v, want %#v", got, want)
	}
}

// R-GNE1-R22W
func TestNamesTreatsWrappedParameterNotFoundAsEmpty(t *testing.T) {
	t.Parallel()

	notFound := &cloud.Error{Service: "ssm", Operation: "GetParameter", Code: "ParameterNotFound"}
	ssm := &listSSM{getErr: errors.Join(errors.New("read failed"), notFound)}
	got, err := Names(context.Background(), listAccount(ssm), "foo.sbx.ikigenba.dev", "crm")
	if err != nil {
		t.Fatalf("Names returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Names = %#v, want none", got)
	}
}

// R-GNE1-R22W
func TestNamesReturnsOtherErrorsUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "cloud error with another code",
			err: &cloud.Error{
				Service:   "ssm",
				Operation: "GetParameter",
				Code:      "AccessDeniedException",
			},
		},
		{name: "unrelated error", err: errors.New("read failed")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ssm := &listSSM{getErr: test.err}
			got, err := Names(context.Background(), listAccount(ssm), "foo.sbx.ikigenba.dev", "crm")
			if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(test.err).Pointer() {
				t.Fatalf("Names error = %T %v, want original error %T %v", err, err, test.err, test.err)
			}
			if got != nil {
				t.Fatalf("Names = %#v, want nil on error", got)
			}
		})
	}
}

// R-GOLY-4TTL
func TestListAndNamesRejectAnythingButJSONObjectOfStrings(t *testing.T) {
	t.Parallel()

	invalid := []string{
		`null`,
		`[]`,
		`{"KEY":1}`,
		`{"KEY":null}`,
		`{"KEY":{"nested":"value"}}`,
		`{"KEY":1,"KEY":"later string"}`,
		`{"KEY":"value"} trailing`,
	}
	const domain = "foo.sbx.ikigenba.dev"
	name := Parameter(domain, "crm")
	for _, value := range invalid {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			listErr := objectErrorFromList(t, domain, name, value)
			nameErr := objectErrorFromNames(t, domain, value)
			for _, got := range []*ObjectError{listErr, nameErr} {
				if got.Parameter != name || got.Reason != invalidObjectReason {
					t.Errorf("ObjectError = %#v, want parameter %q and reason %q", got, name, invalidObjectReason)
				}
				if got.Error() != name+": "+invalidObjectReason {
					t.Errorf("error text = %q", got.Error())
				}
			}
		})
	}
}

func objectErrorFromList(t *testing.T, domain, name, value string) *ObjectError {
	t.Helper()
	ssm := &listSSM{parameters: []cloud.Parameter{{Name: name, Value: value}}}
	_, err := List(context.Background(), listAccount(ssm), domain)
	if reflect.TypeOf(err) != reflect.TypeOf((*ObjectError)(nil)) {
		t.Fatalf("List error = %T %v, want direct *ObjectError", err, err)
	}
	var objectError *ObjectError
	if !errors.As(err, &objectError) {
		t.Fatalf("List error = %T %v, want *ObjectError", err, err)
	}
	return objectError
}

func objectErrorFromNames(t *testing.T, domain, value string) *ObjectError {
	t.Helper()
	ssm := &listSSM{value: value}
	_, err := Names(context.Background(), listAccount(ssm), domain, "crm")
	if reflect.TypeOf(err) != reflect.TypeOf((*ObjectError)(nil)) {
		t.Fatalf("Names error = %T %v, want direct *ObjectError", err, err)
	}
	var objectError *ObjectError
	if !errors.As(err, &objectError) {
		t.Fatalf("Names error = %T %v, want *ObjectError", err, err)
	}
	return objectError
}

func TestListAndNamesNeverYieldSecretValues(t *testing.T) {
	t.Parallel()

	const listSentinel = "list-secret-value-sentinel"
	const namesSentinel = "names-secret-value-sentinel"
	const domain = "foo.sbx.ikigenba.dev"
	name := Parameter(domain, "crm")
	ssm := &listSSM{
		parameters: []cloud.Parameter{{Name: name, Value: `{"CRM_TOKEN":"` + listSentinel + `"}`}},
		value:      `{"API_SECRET":"` + namesSentinel + `"}`,
	}
	entries, err := List(context.Background(), listAccount(ssm), domain)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	names, err := Names(context.Background(), listAccount(ssm), domain, "crm")
	if err != nil {
		t.Fatalf("Names returned error: %v", err)
	}
	wantEntries := []Entry{{App: "crm", Keys: []string{"CRM_TOKEN"}}}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("List entries = %#v, want %#v", entries, wantEntries)
	}
	wantNames := []string{"API_SECRET"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Names = %#v, want %#v", names, wantNames)
	}

	returned := []string{entries[0].App}
	returned = append(returned, entries[0].Keys...)
	returned = append(returned, names...)
	for _, sentinel := range []string{listSentinel, namesSentinel} {
		if strings.Contains(strings.Join(returned, "\n"), sentinel) {
			t.Fatalf("List or Names exposed sentinel %q: entries %#v, names %#v", sentinel, entries, names)
		}
	}
}
