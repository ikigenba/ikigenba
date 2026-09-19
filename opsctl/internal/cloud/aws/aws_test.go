package aws

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
)

var _ cloud.Client = (*client)(nil)

// R-DLFB-Q2LP
func TestInjectedProviderInterfacesContainOnlyOperationalMethods(t *testing.T) {
	s3Interface := reflect.TypeFor[s3API]()
	ssmInterface := reflect.TypeFor[ssmAPI]()
	if s3Interface.NumMethod() != 3 || s3Interface.Method(0).Name != "GetObject" ||
		s3Interface.Method(1).Name != "ListObjectsV2" || s3Interface.Method(2).Name != "PutObject" {
		t.Fatalf("s3API methods = %v", interfaceMethodNames(s3Interface))
	}
	if ssmInterface.NumMethod() != 1 || ssmInterface.Method(0).Name != "GetParameter" {
		t.Fatalf("ssmAPI methods = %v", interfaceMethodNames(ssmInterface))
	}
}

func interfaceMethodNames(interfaceType reflect.Type) []string {
	names := make([]string, interfaceType.NumMethod())
	for index := range names {
		names[index] = interfaceType.Method(index).Name
	}
	return names
}

type fakeS3 struct {
	get  func(context.Context, *awss3.GetObjectInput) (*awss3.GetObjectOutput, error)
	put  func(context.Context, *awss3.PutObjectInput) (*awss3.PutObjectOutput, error)
	list func(context.Context, *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error)
}

func (fake *fakeS3) GetObject(ctx context.Context, input *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	if fake.get == nil {
		panic("unexpected GetObject")
	}
	return fake.get(ctx, input)
}

func (fake *fakeS3) PutObject(ctx context.Context, input *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	if fake.put == nil {
		panic("unexpected PutObject")
	}
	return fake.put(ctx, input)
}

func (fake *fakeS3) ListObjectsV2(ctx context.Context, input *awss3.ListObjectsV2Input, _ ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error) {
	if fake.list == nil {
		panic("unexpected ListObjectsV2")
	}
	return fake.list(ctx, input)
}

type fakeSSM struct {
	get func(context.Context, *awsssm.GetParameterInput) (*awsssm.GetParameterOutput, error)
}

func (fake *fakeSSM) GetParameter(ctx context.Context, input *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
	if fake.get == nil {
		panic("unexpected GetParameter")
	}
	return fake.get(ctx, input)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (body *trackedBody) Close() error {
	body.closed = true
	return nil
}

// R-DLFB-Q2LP R-DMN8-3UCE
func TestOpenUsesRegionAndConcreteClients(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "ignored-region")
	t.Setenv("AWS_DEFAULT_REGION", "ignored-default-region")

	opened, err := Open(t.Context(), "eu-north-1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	actual, ok := opened.(*client)
	if !ok {
		t.Fatalf("Open returned %T", opened)
	}
	s3Client, ok := actual.s3.(*awss3.Client)
	if !ok || s3Client.Options().Region != "eu-north-1" || !s3Client.Options().UsePathStyle {
		t.Fatalf("S3 client = %T options %#v", actual.s3, s3Client.Options())
	}
	ssmClient, ok := actual.ssm.(*awsssm.Client)
	if !ok || ssmClient.Options().Region != "eu-north-1" {
		t.Fatalf("SSM client = %T region %q", actual.ssm, ssmClient.Options().Region)
	}
}

// R-DMN8-3UCE R-DP30-VDTS
func TestOpenPreservesEndpointConfigurationAndUsesPathStyle(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: time.Second}
	t.Cleanup(func() {
		_ = server.Close()
	})
	requests := make(chan *http.Request, 1)
	server.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(request.Context())
		_, _ = writer.Write([]byte("object body"))
	})
	go func() {
		_ = server.Serve(listener)
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	endpoint := "http://localhost:" + strconv.Itoa(port)
	t.Setenv("AWS_ENDPOINT_URL_S3", endpoint)
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	opened, err := Open(t.Context(), "us-east-2")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := opened.GetObject(t.Context(), "s3://bucket.with.dots/a%20key")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	data, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || string(data) != "object body" {
		t.Fatalf("response body = %q, read %v, close %v", data, readErr, closeErr)
	}
	select {
	case request := <-requests:
		if request.Host != "localhost:"+strconv.Itoa(port) || request.URL.EscapedPath() != "/bucket.with.dots/a%20key" {
			t.Fatalf("request host/path = %q %q", request.Host, request.URL.EscapedPath())
		}
	case <-t.Context().Done():
		t.Fatal("fake endpoint received no request")
	}
}

// R-DQAX-95KH R-DRIT-MXB6 R-DV6I-S8J9
func TestGetObjectURIResponseAndNotFound(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("object bytes")}
	calls := 0
	s3Client := &fakeS3{get: func(ctx context.Context, input *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) {
		calls++
		if ctx != t.Context() || awssdk.ToString(input.Bucket) != "bucket" || awssdk.ToString(input.Key) != "/a/../b c?#" {
			t.Fatalf("GetObject input = %#v", input)
		}
		return &awss3.GetObjectOutput{Body: body}, nil
	}}
	client := newClient(s3Client, &fakeSSM{})
	got, err := client.GetObject(t.Context(), "s3://bucket//a/../b%20c%3F%23")
	if err != nil || got != body || body.closed {
		t.Fatalf("GetObject = %v, %v; closed=%v", got, err, body.closed)
	}

	providerText := "provider response must remain secret"
	s3Client.get = func(context.Context, *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) {
		return nil, &s3types.NoSuchKey{Message: awssdk.String(providerText)}
	}
	_, err = client.GetObject(t.Context(), "s3://bucket/key")
	if !errors.Is(err, cloud.ErrNotFound) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("NoSuchKey error = %v", err)
	}

	generic := &apiError{code: "NoSuchKey", message: providerText}
	s3Client.get = func(context.Context, *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) { return nil, generic }
	_, err = client.GetObject(t.Context(), "s3://bucket/key")
	if errors.Is(err, cloud.ErrNotFound) || !errors.Is(err, generic) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("generic NoSuchKey error = %v", err)
	}

	s3Client.get = func(context.Context, *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) { return nil, nil }
	_, err = client.GetObject(t.Context(), "s3://bucket/key")
	if err == nil || !strings.Contains(err.Error(), "invalid response") {
		t.Fatalf("nil GetObject response error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("initial GetObject calls = %d", calls)
	}
}

// R-DQAX-95KH
func TestObjectURIsRejectInvalidFormsBeforeProviderCalls(t *testing.T) {
	var calls int
	s3Client := &fakeS3{
		get: func(context.Context, *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) {
			calls++
			return nil, errors.New("unexpected")
		},
		put: func(context.Context, *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
			calls++
			return nil, errors.New("unexpected")
		},
		list: func(context.Context, *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
			calls++
			return nil, errors.New("unexpected")
		},
	}
	client := newClient(s3Client, &fakeSSM{})
	invalid := []string{
		"", "bucket/key", "https://bucket/key", "s3:bucket/key", "s3:///key",
		"s3://user@bucket/key", "s3://bucket:443/key", "s3://bucket:/key", "s3://bucket/key?version=1",
		"s3://bucket/key?", "s3://bucket/key#fragment", "s3://bucket/key#",
	}
	for _, uri := range invalid {
		if _, err := client.GetObject(t.Context(), uri); err == nil {
			t.Errorf("GetObject(%q) succeeded", uri)
		}
		if err := client.PutObject(t.Context(), uri, strings.NewReader("body")); err == nil {
			t.Errorf("PutObject(%q) succeeded", uri)
		}
		if _, err := client.ListObjects(t.Context(), uri); err == nil {
			t.Errorf("ListObjects(%q) succeeded", uri)
		}
	}
	for _, uri := range []string{"s3://bucket", "s3://bucket/"} {
		if _, err := client.GetObject(t.Context(), uri); err == nil {
			t.Errorf("GetObject(%q) accepted empty key", uri)
		}
		if err := client.PutObject(t.Context(), uri, strings.NewReader("body")); err == nil {
			t.Errorf("PutObject(%q) accepted empty key", uri)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid URIs made %d provider calls", calls)
	}
	s3Client.list = func(_ context.Context, input *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
		calls++
		if awssdk.ToString(input.Bucket) != "bucket" || awssdk.ToString(input.Prefix) != "" {
			t.Fatalf("empty-prefix input = %#v", input)
		}
		return &awss3.ListObjectsV2Output{}, nil
	}
	objects, err := client.ListObjects(t.Context(), "s3://bucket")
	if err != nil || objects != nil || calls != 1 {
		t.Fatalf("empty-prefix ListObjects = %#v, %v; calls=%d", objects, err, calls)
	}
}

// R-DSQQ-0P1V
func TestListObjectsPaginatesValidatesAndCanonicalizes(t *testing.T) {
	firstTime := time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC)
	secondTime := firstTime.Add(time.Minute)
	call := 0
	s3Client := &fakeS3{list: func(ctx context.Context, input *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
		call++
		if ctx != t.Context() || awssdk.ToString(input.Bucket) != "bucket" || awssdk.ToString(input.Prefix) != "/a/../prefix " || input.Delimiter != nil {
			t.Fatalf("ListObjectsV2 input = %#v", input)
		}
		switch call {
		case 1:
			if input.ContinuationToken != nil {
				t.Fatalf("first continuation = %q", awssdk.ToString(input.ContinuationToken))
			}
			return &awss3.ListObjectsV2Output{
				Contents:    []s3types.Object{{Key: awssdk.String("z/../last ?#"), Size: awssdk.Int64(7), LastModified: &secondTime}},
				IsTruncated: awssdk.Bool(true), NextContinuationToken: awssdk.String("next-token"),
			}, nil
		case 2:
			if awssdk.ToString(input.ContinuationToken) != "next-token" {
				t.Fatalf("second continuation = %q", awssdk.ToString(input.ContinuationToken))
			}
			return &awss3.ListObjectsV2Output{
				Contents:    []s3types.Object{{Key: awssdk.String("/first"), Size: awssdk.Int64(3), LastModified: &firstTime}},
				IsTruncated: awssdk.Bool(false),
			}, nil
		default:
			t.Fatalf("unexpected page %d", call)
			return nil, nil
		}
	}}
	client := newClient(s3Client, &fakeSSM{})
	objects, err := client.ListObjects(t.Context(), "s3://bucket//a/../prefix%20")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	want := []cloud.Object{
		{URI: "s3://bucket/z/../last%20%3F%23", Size: 7, Modified: secondTime},
		{URI: "s3://bucket//first", Size: 3, Modified: firstTime},
	}
	if !reflect.DeepEqual(objects, want) || call != 2 {
		t.Fatalf("ListObjects = %#v over %d calls, want %#v", objects, call, want)
	}
}

// R-DSQQ-0P1V R-DV6I-S8J9
func TestListObjectsRejectsMalformedAndPartialResponses(t *testing.T) {
	now := time.Now()
	valid := s3types.Object{Key: awssdk.String("key"), Size: awssdk.Int64(1), LastModified: &now}
	malformed := []struct {
		name   string
		object s3types.Object
	}{
		{name: "missing key", object: s3types.Object{Size: awssdk.Int64(1), LastModified: &now}},
		{name: "missing size", object: s3types.Object{Key: awssdk.String("key"), LastModified: &now}},
		{name: "missing modification time", object: s3types.Object{Key: awssdk.String("key"), Size: awssdk.Int64(1)}},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(&fakeS3{list: func(context.Context, *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
				return &awss3.ListObjectsV2Output{Contents: []s3types.Object{test.object}}, nil
			}}, &fakeSSM{})
			objects, err := client.ListObjects(t.Context(), "s3://bucket/prefix")
			if err == nil || objects != nil {
				t.Fatalf("ListObjects = %#v, %v", objects, err)
			}
		})
	}

	providerText := "later provider response secret"
	providerErr := errors.New(providerText)
	calls := 0
	client := newClient(&fakeS3{list: func(context.Context, *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
		calls++
		if calls == 1 {
			return &awss3.ListObjectsV2Output{Contents: []s3types.Object{valid}, IsTruncated: awssdk.Bool(true), NextContinuationToken: awssdk.String("next")}, nil
		}
		return nil, providerErr
	}}, &fakeSSM{})
	objects, err := client.ListObjects(t.Context(), "s3://bucket/prefix")
	if objects != nil || !errors.Is(err, providerErr) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("later-page failure = %#v, %v", objects, err)
	}
}

// R-DTYM-EGSK R-DV6I-S8J9
func TestReadSecretsStrictJSONMappingAndSecrecy(t *testing.T) {
	parameter := "/ikigenba/host/notes"
	var source string
	ssmClient := &fakeSSM{get: func(ctx context.Context, input *awsssm.GetParameterInput) (*awsssm.GetParameterOutput, error) {
		if ctx != t.Context() || awssdk.ToString(input.Name) != parameter || !awssdk.ToBool(input.WithDecryption) {
			t.Fatalf("GetParameter input = %#v", input)
		}
		return &awsssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: awssdk.String(source)}}, nil
	}}
	client := newClient(&fakeS3{}, ssmClient)

	source = `{ "EMPTY": "", "TOKEN": "very-secret-value" }`
	secrets, err := client.ReadSecrets(t.Context(), parameter)
	want := map[string]string{"EMPTY": "", "TOKEN": "very-secret-value"}
	if err != nil || !reflect.DeepEqual(secrets, want) {
		t.Fatalf("ReadSecrets = %#v, %v", secrets, err)
	}
	source = `{}`
	secrets, err = client.ReadSecrets(t.Context(), parameter)
	if err != nil || secrets == nil || len(secrets) != 0 {
		t.Fatalf("empty ReadSecrets = %#v, %v", secrets, err)
	}

	invalid := []string{
		`null`, `[]`, `"text"`, `{"A":1}`, `{"A":null}`, `{"A":"first","A":"duplicate-secret"}`,
		`{"A":"secret"} {}`, `{"A":"secret"} trailing`, string([]byte{'{', '"', 'A', '"', ':', '"', 0xff, '"', '}'}),
	}
	for index, value := range invalid {
		source = value
		secrets, err = client.ReadSecrets(t.Context(), parameter)
		if err == nil || secrets != nil {
			t.Errorf("invalid[%d] = %#v, %v", index, secrets, err)
			continue
		}
		for _, forbidden := range []string{"duplicate-secret", "very-secret-value", value} {
			if forbidden != "" && strings.Contains(err.Error(), forbidden) {
				t.Errorf("invalid[%d] error exposed %q: %v", index, forbidden, err)
			}
		}
	}

	providerText := "provider response secret"
	notFound := &ssmtypes.ParameterNotFound{Message: awssdk.String(providerText)}
	ssmClient.get = func(context.Context, *awsssm.GetParameterInput) (*awsssm.GetParameterOutput, error) {
		return nil, notFound
	}
	_, err = client.ReadSecrets(t.Context(), parameter)
	if !errors.Is(err, cloud.ErrNotFound) || !errors.Is(err, notFound) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("ParameterNotFound error = %v", err)
	}
	generic := &apiError{code: "ParameterNotFound", message: providerText}
	ssmClient.get = func(context.Context, *awsssm.GetParameterInput) (*awsssm.GetParameterOutput, error) {
		return nil, generic
	}
	_, err = client.ReadSecrets(t.Context(), parameter)
	if errors.Is(err, cloud.ErrNotFound) || !errors.Is(err, generic) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("generic ParameterNotFound error = %v", err)
	}
	for _, output := range []*awsssm.GetParameterOutput{nil, {}, {Parameter: &ssmtypes.Parameter{}}} {
		ssmClient.get = func(context.Context, *awsssm.GetParameterInput) (*awsssm.GetParameterOutput, error) {
			return output, nil
		}
		secrets, err = client.ReadSecrets(t.Context(), parameter)
		if err == nil || secrets != nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("malformed GetParameter response = %#v, %v", secrets, err)
		}
	}
}

// R-AQ2K-9UAJ
func TestPutObjectUsesOneConditionalCompleteUpload(t *testing.T) {
	body := strings.NewReader("complete object body")
	calls := 0
	client := newClient(&fakeS3{put: func(ctx context.Context, input *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
		calls++
		if ctx != t.Context() || awssdk.ToString(input.Bucket) != "bucket" || awssdk.ToString(input.Key) != "/a/../key ?" ||
			awssdk.ToString(input.IfNoneMatch) != "*" || input.Body != body {
			t.Fatalf("PutObject input = %#v", input)
		}
		data, err := io.ReadAll(input.Body)
		if err != nil || string(data) != "complete object body" {
			t.Fatalf("PutObject body = %q, %v", data, err)
		}
		return &awss3.PutObjectOutput{}, nil
	}}, &fakeSSM{})
	if err := client.PutObject(t.Context(), "s3://bucket//a/../key%20%3F", body); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if calls != 1 {
		t.Fatalf("PutObject calls = %d", calls)
	}
}

type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string       { return e.err.Error() }
func (e *statusError) Unwrap() error       { return e.err }
func (e *statusError) HTTPStatusCode() int { return e.status }

type apiError struct {
	code    string
	message string
}

func (e *apiError) Error() string     { return e.message }
func (e *apiError) ErrorCode() string { return e.code }

// R-AQ2K-9UAJ
func TestPutObjectMapsOnlyExactConditionalErrorsWithoutRetry(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		status        int
		wantCollision bool
	}{
		{name: "precondition", code: "PreconditionFailed", status: http.StatusPreconditionFailed, wantCollision: true},
		{name: "conflict", code: "ConditionalRequestConflict", status: http.StatusConflict, wantCollision: true},
		{name: "wrong precondition status", code: "PreconditionFailed", status: http.StatusConflict},
		{name: "wrong conflict status", code: "ConditionalRequestConflict", status: http.StatusPreconditionFailed},
		{name: "unrelated 412", code: "Other", status: http.StatusPreconditionFailed},
		{name: "unrelated 409", code: "Other", status: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerText := "provider response secret"
			apiErr := &apiError{code: test.code, message: providerText}
			providerErr := &statusError{status: test.status, err: apiErr}
			calls := 0
			client := newClient(&fakeS3{put: func(context.Context, *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
				calls++
				return nil, providerErr
			}}, &fakeSSM{})
			err := client.PutObject(t.Context(), "s3://bucket/key", strings.NewReader("body"))
			if errors.Is(err, cloud.ErrAlreadyExists) != test.wantCollision || !errors.Is(err, providerErr) || calls != 1 {
				t.Fatalf("PutObject error = %v, calls = %d", err, calls)
			}
			if strings.Contains(err.Error(), providerText) {
				t.Fatalf("PutObject error exposed provider response: %v", err)
			}
		})
	}

}

// R-AQ2K-9UAJ
func TestPutObjectConcurrentCreateHasOneWinner(t *testing.T) {
	const writers = 24
	var mu sync.Mutex
	stored := make(map[string]string)
	calls := 0
	client := newClient(&fakeS3{put: func(_ context.Context, input *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
		data, err := io.ReadAll(input.Body)
		if err != nil {
			return nil, err
		}
		key := awssdk.ToString(input.Bucket) + "/" + awssdk.ToString(input.Key)
		mu.Lock()
		defer mu.Unlock()
		calls++
		if awssdk.ToString(input.IfNoneMatch) != "*" {
			t.Errorf("IfNoneMatch = %q", awssdk.ToString(input.IfNoneMatch))
		}
		if _, exists := stored[key]; exists {
			return nil, &statusError{status: http.StatusPreconditionFailed, err: &apiError{code: "PreconditionFailed"}}
		}
		stored[key] = string(data)
		return &awss3.PutObjectOutput{}, nil
	}}, &fakeSSM{})

	start := make(chan struct{})
	errorsByWriter := make([]error, writers)
	var wait sync.WaitGroup
	for index := range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errorsByWriter[index] = client.PutObject(t.Context(), "s3://bucket/key", strings.NewReader("writer-"+strconv.Itoa(index)))
		}()
	}
	close(start)
	wait.Wait()

	successes := 0
	collisions := 0
	for _, err := range errorsByWriter {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, cloud.ErrAlreadyExists):
			collisions++
		default:
			t.Errorf("unexpected writer error: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	values := make([]string, 0, len(stored))
	for _, value := range stored {
		values = append(values, value)
	}
	slices.Sort(values)
	if successes != 1 || collisions != writers-1 || calls != writers || len(stored) != 1 || !strings.HasPrefix(values[0], "writer-") {
		t.Fatalf("successes=%d collisions=%d calls=%d stored=%v", successes, collisions, calls, stored)
	}
}
