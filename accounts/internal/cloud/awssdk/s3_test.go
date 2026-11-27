package awssdk_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	backupBucket = "test-backup-bucket"
	backupPrefix = "accounts/June + July/"
)

func TestListKeys(t *testing.T) {
	// R-L529-H8KZ
	pages := []string{
		`<IsTruncated>true</IsTruncated><NextContinuationToken>next+/one</NextContinuationToken>`,
		`<IsTruncated>true</IsTruncated><NextContinuationToken>next+/two</NextContinuationToken>
			<Contents><Key>z/key</Key></Contents><Contents><Key>a&amp;b</Key></Contents>`,
		`<IsTruncated>false</IsTruncated><Contents><Key>final/key</Key></Contents>`,
	}
	for _, stop := range []bool{false, true} {
		requests := 0
		tokens := []string{"", "next+/one", "next+/two"}
		var got [][]string
		c := open(t, func(r *http.Request) (*http.Response, error) {
			if requests >= len(pages) || stop && requests >= 2 {
				t.Fatal("requested a page after listing stopped")
			}
			if requests > 0 && len(got) != requests-1 {
				t.Fatalf("requested page %d after receiving %d callback calls", requests+1, len(got))
			}
			checkS3ListRequest(t, r, cloud.MaxKeysPerPage, tokens[requests])
			body := pages[requests]
			requests++
			return s3ListResponse(body), nil
		})
		err := c.ListKeys(t.Context(), backupBucket, backupPrefix, func(keys []string) bool {
			got = append(got, slices.Clone(keys))
			return !stop
		})
		want := [][]string{{"z/key", "a&b"}, {"final/key"}}
		if stop {
			want = want[:1]
		}
		if err != nil || requests != len(want)+1 || !reflect.DeepEqual(got, want) {
			t.Errorf("ListKeys = pages %q, requests %d, error %v, want %q and %d requests", got, requests, err, want, len(want)+1)
		}
	}
}

func TestHasKeys(t *testing.T) {
	// R-T9A1-XJ4I
	for _, test := range []struct {
		name, final string
		want        bool
	}{
		{"key on untruncated page", "<IsTruncated>false</IsTruncated><Contents><Key>matched</Key></Contents>", true},
		{"key on truncated page", "<IsTruncated>true</IsTruncated><Contents><Key>matched</Key></Contents><NextContinuationToken>unused</NextContinuationToken>", true},
		{"no key", "<IsTruncated>false</IsTruncated>", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pages := []string{
				"<IsTruncated>true</IsTruncated><NextContinuationToken>next+/one</NextContinuationToken>",
				"<IsTruncated>true</IsTruncated><NextContinuationToken>next+/two</NextContinuationToken>",
				test.final,
			}
			tokens := []string{"", "next+/one", "next+/two"}
			requests := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				if requests >= len(pages) {
					t.Fatal("requested a page after finding a key or reaching the end")
				}
				checkS3ListRequest(t, r, 1, tokens[requests])
				body := pages[requests]
				requests++
				return s3ListResponse(body), nil
			})
			got, err := c.HasKeys(t.Context(), backupBucket, backupPrefix)
			if err != nil || requests != len(pages) || got != test.want {
				t.Errorf("HasKeys = %v, %v after %d requests, want %v after %d", got, err, requests, test.want, len(pages))
			}
		})
	}
}

func checkS3ListRequest(t *testing.T, r *http.Request, maxKeys int, token string) {
	t.Helper()
	checkS3Request(t, r, http.MethodGet)
	query := map[string]string{"list-type": "2", "max-keys": strconv.Itoa(maxKeys), "prefix": backupPrefix}
	if token != "" {
		query["continuation-token"] = token
	}
	checkForm(t, r.URL.Query(), query)
}

func checkS3Request(t *testing.T, r *http.Request, method string) {
	t.Helper()
	host := backupBucket + ".s3." + region + ".amazonaws.com"
	if r.Method != method || r.URL.Scheme != "https" || r.URL.Host != host || r.URL.Path != "/" {
		t.Errorf("S3 request = %s %s, want %s https://%s/", r.Method, r.URL, method, host)
	}
}

func s3ListResponse(body string) *http.Response {
	return xmlResponse(200, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`+body+`</ListBucketResult>`)
}

func TestDeleteKeysRejectsInvalidCount(t *testing.T) {
	// R-L6A5-V0BO
	c := open(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid list of keys was sent to S3")
		return nil, nil
	})
	for _, test := range []struct {
		bucket string
		keys   []string
	}{
		{"b", nil},
		{"", []string{}},
		{backupBucket, make([]string, cloud.MaxKeysPerPage+1)},
	} {
		err := c.DeleteKeys(t.Context(), test.bucket, test.keys)
		checkError(t, err, "s3", "DeleteObjects", test.bucket, "MalformedXML", cloud.ClassRefused)
		if test.bucket == "b" && err.Error() != "s3 DeleteObjects b: MalformedXML" {
			t.Errorf("error text = %q", err)
		}
	}
}

func TestDeleteKeys(t *testing.T) {
	// R-HC08-NWXL
	largest := make([]string, cloud.MaxKeysPerPage)
	for i := range largest {
		largest[i] = "object " + strconv.Itoa(i)
	}
	for _, test := range []struct {
		name, response, code string
		keys                 []string
		class                cloud.Class
	}{
		{"full page", "", "", largest, cloud.ClassRefused},
		{"first failure", `<Error><Key>first</Key><Code>AccessDenied</Code><Message>private message</Message></Error>
			<Error><Key>second</Key><Code>InternalError</Code></Error>`, "AccessDenied", []string{"first", "second"}, cloud.ClassRefused},
		{"temporary failure", `<Error><Key>amp&amp;ersand</Key><Code>SlowDown</Code></Error>`, "SlowDown", []string{"amp&ersand"}, cloud.ClassTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				requests++
				checkS3Request(t, r, http.MethodPost)
				checkForm(t, r.URL.Query(), map[string]string{"delete": ""})
				var in struct {
					XMLName xml.Name `xml:"Delete"`
					Quiet   bool
					Objects []struct {
						Key   string
						Extra []struct{ XMLName xml.Name } `xml:",any"`
					} `xml:"Object"`
				}
				if err := xml.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Fatal(err)
				}
				keys := make([]string, 0, len(in.Objects))
				for _, object := range in.Objects {
					if len(object.Extra) != 0 {
						t.Errorf("unexpected fields in delete object: %+v", object.Extra)
					}
					keys = append(keys, object.Key)
				}
				if !in.Quiet || !slices.Equal(keys, test.keys) {
					t.Errorf("delete requested quiet %v and %d keys, expected quiet mode and the %d supplied keys in order", in.Quiet, len(keys), len(test.keys))
				}
				return xmlResponse(200, "<DeleteResult>"+test.response+"</DeleteResult>"), nil
			})
			err := c.DeleteKeys(t.Context(), backupBucket, test.keys)
			if requests != 1 {
				t.Errorf("DeleteKeys sent %d requests, want 1", requests)
			}
			if test.code != "" {
				checkError(t, err, "s3", "DeleteObjects", backupBucket, test.code, test.class)
			} else if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestS3OperationMappings(t *testing.T) {
	// R-YMVW-I8I5
	for _, test := range []struct {
		name, op, method string
		call             func(*testing.T, context.Context, cloud.Cloud) error
	}{
		{"list", "ListObjectsV2", http.MethodGet, func(t *testing.T, ctx context.Context, c cloud.Cloud) error {
			return c.ListKeys(ctx, backupBucket, backupPrefix, func([]string) bool {
				t.Error("failed listing supplied keys")
				return false
			})
		}},
		{"check", "ListObjectsV2", http.MethodGet, func(_ *testing.T, ctx context.Context, c cloud.Cloud) error {
			_, err := c.HasKeys(ctx, backupBucket, backupPrefix)
			return err
		}},
		{"delete", "DeleteObjects", http.MethodPost, func(_ *testing.T, ctx context.Context, c cloud.Cloud) error {
			return c.DeleteKeys(ctx, backupBucket, []string{"private key name", "another key"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				requests++
				checkS3Request(t, r, test.method)
				query := r.URL.Query()
				if test.method == http.MethodGet && query.Get("list-type") != "2" || test.method == http.MethodPost && !query.Has("delete") {
					t.Errorf("unexpected query for %s: %v", test.op, query)
				}
				return xmlResponse(403, `<Error><Code>AccessDenied</Code><Message>private message</Message></Error>`), nil
			})
			err := test.call(t, t.Context(), c)
			checkError(t, err, "s3", test.op, backupBucket, "AccessDenied", cloud.ClassRefused)
			if requests != 1 || err.Error() != "s3 "+test.op+" "+backupBucket+": AccessDenied" {
				t.Errorf("requests = %d, error = %v", requests, err)
			}
		})
	}
}
