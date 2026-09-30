package cloud_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
)

// minimalClient implements only the methods cloud.Client declares.
type minimalClient struct{}

func (minimalClient) GetObject(_ context.Context, uri string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(uri)), nil
}

func (minimalClient) PutObject(context.Context, string, io.Reader) error {
	return nil
}

func (minimalClient) ListObjects(_ context.Context, prefix string) ([]cloud.Object, error) {
	return []cloud.Object{{URI: prefix}}, nil
}

func (minimalClient) ReadSecrets(_ context.Context, parameter string) (map[string]string, error) {
	return map[string]string{"parameter": parameter}, nil
}

func TestEnvFields(t *testing.T) {
	// R-Y929-PBGN
	env := cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		if region != "us-east-2" {
			t.Fatalf("Open region = %q", region)
		}
		return minimalClient{}, nil
	}}
	open := typed[func(ctx context.Context, region string) (cloud.Client, error)](env.Open)
	client, err := open(context.Background(), "us-east-2")
	if err != nil || client == nil {
		t.Fatalf("Env.Open = (%v, %v)", client, err)
	}
}

func TestClientMethods(t *testing.T) {
	// R-5P36-N5FV
	var client cloud.Client = minimalClient{}
	ctx := context.Background()
	reader, err := client.GetObject(ctx, "s3://bucket/key")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err != nil || closeErr != nil || string(body) != "s3://bucket/key" {
		t.Fatalf("GetObject body = %q, err = %v, close = %v", body, err, closeErr)
	}
	if err := client.PutObject(ctx, "s3://bucket/key", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	objects, err := client.ListObjects(ctx, "s3://bucket/")
	if err != nil || len(objects) != 1 || objects[0].URI != "s3://bucket/" {
		t.Fatalf("ListObjects = (%v, %v)", objects, err)
	}
	secrets, err := client.ReadSecrets(ctx, "/param")
	if err != nil || secrets["parameter"] != "/param" {
		t.Fatalf("ReadSecrets = (%v, %v)", secrets, err)
	}
}

func TestObjectFields(t *testing.T) {
	// R-YAA6-337C
	modified := time.Unix(100, 0)
	object := cloud.Object{URI: "s3://bucket/key", Size: 42, Modified: modified}
	uri := typed[string](object.URI)
	size := typed[int64](object.Size)
	when := typed[time.Time](object.Modified)
	if uri != "s3://bucket/key" || size != 42 || !when.Equal(modified) {
		t.Fatalf("Object = %+v", object)
	}
}

func TestErrorSentinels(t *testing.T) {
	// R-AOUN-W2JU
	missing := cloud.ErrNotFound
	collision := cloud.ErrAlreadyExists
	if missing == nil || collision == nil {
		t.Fatal("cloud sentinels must be non-nil errors")
	}
	for _, sentinel := range []error{missing, collision} {
		wrapped := fmt.Errorf("object operation: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("wrapped error does not match %v", sentinel)
		}
		for _, distinct := range []error{errors.New("access denied"), errors.New("transport failed")} {
			if errors.Is(distinct, sentinel) {
				t.Errorf("%v matches sentinel %v", distinct, sentinel)
			}
		}
	}
	if errors.Is(missing, collision) || errors.Is(collision, missing) {
		t.Fatal("not-found and collision sentinels must be distinct")
	}
}

// typed returns v as a T; the call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }
