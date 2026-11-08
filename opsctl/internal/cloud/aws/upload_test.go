package aws

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
)

const uploadPattern = "streamed upload content"

// generatedUpload exposes an unknown-length, non-seekable body without holding it.
type generatedUpload struct {
	eofReached bool
	left       int
	yielded    atomic.Int64
	reads      int
	inFlight   *atomic.Bool
	t          *testing.T
}

func (r *generatedUpload) Read(p []byte) (int, error) {
	r.reads++
	if r.inFlight != nil && r.inFlight.Load() {
		r.t.Error("body read while request in flight")
	}
	if r.left == 0 {
		r.eofReached = true
		return 0, io.EOF
	}
	n := min(len(p), r.left)
	offset := r.yielded.Load()
	for i := range p[:n] {
		p[i] = uploadPattern[(offset+int64(i))%int64(len(uploadPattern))]
	}
	r.left -= n
	r.yielded.Add(int64(n))
	return n, nil
}

func uploadChecksum(t *testing.T, body io.Reader, size int64, offset int64) string {
	t.Helper()
	seeker, ok := body.(io.Seeker)
	if !ok {
		t.Fatalf("body %T is not seekable", body)
	}
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		t.Fatalf("body size=%d want=%d error=%v", len(data), size, err)
	}
	for i, b := range data {
		if b != uploadPattern[(offset+int64(i))%int64(len(uploadPattern))] {
			t.Fatalf("wrong byte at %d", offset+int64(i))
		}
	}
	if pos, err := seeker.Seek(0, io.SeekStart); err != nil || pos != 0 {
		t.Fatalf("rewind=%d %v", pos, err)
	}
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], crc32.ChecksumIEEE(data))
	return base64.StdEncoding.EncodeToString(encoded[:])
}

// R-P9CI-Z4IC R-PAKF-CW91
func TestPutObjectSinglePartBoundaries(t *testing.T) {
	if PartSize != 16777216 {
		t.Fatalf("PartSize=%d", PartSize)
	}
	for _, size := range []int{0, 9, PartSize - 1, PartSize} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := &generatedUpload{left: size}
			calls := 0
			fake := &fakeS3{put: func(_ context.Context, in *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
				calls++
				if awssdk.ToString(in.Bucket) != "bucket" || awssdk.ToString(in.Key) != "/key ?" || awssdk.ToString(in.IfNoneMatch) != "*" || awssdk.ToInt64(in.ContentLength) != int64(size) {
					t.Fatalf("single input=%#v", in)
				}
				_ = uploadChecksum(t, in.Body, int64(size), 0)
				return &awss3.PutObjectOutput{}, nil
			}}
			if err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket//key%20%3F", body); err != nil || calls != 1 {
				t.Fatalf("PutObject=%v calls=%d", err, calls)
			}
		})
	}
}

// R-P9CI-Z4IC R-VR57-1E1E R-VMW2-WJUY
func TestPutObjectMultipartOrderingBoundAndChecksums(t *testing.T) {
	for _, size := range []int{PartSize + 1, 2 * PartSize, 2*PartSize + 31} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var inFlight atomic.Bool
			body := &generatedUpload{left: size, inFlight: &inFlight, t: t}
			begin := func() func() {
				if !inFlight.CompareAndSwap(false, true) {
					t.Fatal("overlapping requests")
				}
				before := body.yielded.Load()
				return func() {
					if body.yielded.Load() != before {
						t.Error("body advanced during request")
					}
					inFlight.Store(false)
				}
			}
			creates, uploads, completes, aborts := 0, 0, 0, 0
			var parts []s3types.CompletedPart
			checkLocation := func(bucket, key, id *string) {
				t.Helper()
				if awssdk.ToString(bucket) != "bucket" || awssdk.ToString(key) != "/key ?" || awssdk.ToString(id) != "upload-id" {
					t.Fatalf("location %v %v %v", bucket, key, id)
				}
			}
			fake := &fakeS3{
				create: func(_ context.Context, in *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
					defer begin()()
					creates++
					if awssdk.ToString(in.Bucket) != "bucket" || awssdk.ToString(in.Key) != "/key ?" {
						t.Fatalf("create location=%#v", in)
					}
					if in.ChecksumAlgorithm != s3types.ChecksumAlgorithmCrc32 {
						t.Fatal("create checksum algorithm")
					}
					return &awss3.CreateMultipartUploadOutput{UploadId: awssdk.String("upload-id")}, nil
				},
				upload: func(_ context.Context, in *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
					defer begin()()
					uploads++
					checkLocation(in.Bucket, in.Key, in.UploadId)
					n := awssdk.ToInt32(in.PartNumber)
					wantSize := min(PartSize, size-(uploads-1)*PartSize)
					if int(n) != uploads || in.ChecksumAlgorithm != s3types.ChecksumAlgorithmCrc32 || awssdk.ToInt64(in.ContentLength) != int64(wantSize) || body.yielded.Load() > int64(uploads*PartSize+1) {
						t.Fatalf("part=%#v yielded=%d", in, body.yielded.Load())
					}
					checksum := uploadChecksum(t, in.Body, int64(wantSize), int64((uploads-1)*PartSize))
					etag := fmt.Sprintf("etag-%d", uploads)
					parts = append(parts, s3types.CompletedPart{PartNumber: awssdk.Int32(n), ETag: &etag, ChecksumCRC32: &checksum})
					return &awss3.UploadPartOutput{ETag: &etag, ChecksumCRC32: &checksum}, nil
				},
				complete: func(_ context.Context, in *awss3.CompleteMultipartUploadInput) (*awss3.CompleteMultipartUploadOutput, error) {
					defer begin()()
					completes++
					checkLocation(in.Bucket, in.Key, in.UploadId)
					if !body.eofReached || body.left != 0 || awssdk.ToString(in.IfNoneMatch) != "*" || in.MultipartUpload == nil || len(in.MultipartUpload.Parts) != len(parts) {
						t.Fatalf("completion=%#v", in)
					}
					for i, p := range in.MultipartUpload.Parts {
						want := parts[i]
						if awssdk.ToInt32(p.PartNumber) != awssdk.ToInt32(want.PartNumber) || awssdk.ToString(p.ETag) != *want.ETag || awssdk.ToString(p.ChecksumCRC32) != *want.ChecksumCRC32 {
							t.Fatalf("part %d=%#v", i, p)
						}
					}
					return &awss3.CompleteMultipartUploadOutput{}, nil
				},
				abort: func(context.Context, *awss3.AbortMultipartUploadInput) (*awss3.AbortMultipartUploadOutput, error) {
					aborts++
					return nil, nil
				},
			}
			if err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket//key%20%3F", body); err != nil {
				t.Fatal(err)
			}
			if creates != 1 || uploads != (size+PartSize-1)/PartSize || completes != 1 || aborts != 0 {
				t.Fatalf("requests create=%d parts=%d complete=%d abort=%d", creates, uploads, completes, aborts)
			}
		})
	}
}

// R-PE84-I7H4
func TestPutObjectStopsAtEveryReadError(t *testing.T) {
	for _, failure := range []error{errors.New("read failed"), io.ErrUnexpectedEOF} {
		for _, offset := range []int{0, PartSize, PartSize + 1, 2*PartSize + 1} {
			t.Run(fmt.Sprintf("%v/%d", failure, offset), func(t *testing.T) {
				fake, events := failureUploadFake()
				body := &boundaryReader{source: &generatedUpload{left: 3*PartSize + 5}, offset: offset, failure: failure, events: events}
				err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket/key", body)
				if !errors.Is(err, failure) || body.readsAfterStop != 0 {
					t.Fatalf("error=%v reads after failure=%d events=%v", err, body.readsAfterStop, *events)
				}
				assertStoppedUpload(t, *events, offset)
			})
		}
	}
}

type boundaryReader struct {
	source         io.Reader
	offset         int
	yielded        int
	failure        error
	cancel         context.CancelFunc
	events         *[]string
	stopped        bool
	readsAfterStop int
}

func (r *boundaryReader) Read(p []byte) (int, error) {
	if r.stopped {
		r.readsAfterStop++
		return 0, r.failure
	}
	if r.yielded == r.offset {
		r.stopped = true
		*r.events = append(*r.events, "stop")
		if r.cancel != nil {
			r.cancel()
			return 0, io.EOF
		}
		return 0, r.failure
	}
	n, err := r.source.Read(p[:min(len(p), r.offset-r.yielded)])
	r.yielded += n
	return n, err
}
func assertStoppedUpload(t *testing.T, events []string, offset int) {
	t.Helper()
	split := slices.Index(events, "stop")
	if split < 0 {
		t.Fatalf("stop not reached: %v", events)
	}
	prefix, suffix := events[:split], events[split+1:]
	if offset <= PartSize && len(prefix) != 0 {
		t.Fatalf("requests before early read failure: %v", events)
	}
	created := slices.Contains(prefix, "create")
	want := ""
	if created {
		want = "abort"
	}
	if strings.Join(suffix, ",") != want {
		t.Fatalf("requests after stop=%v want=%q", suffix, want)
	}
}

func failureUploadFake() (*fakeS3, *[]string) {
	events := []string{}
	fake := &fakeS3{
		put: func(context.Context, *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) {
			events = append(events, "put")
			return &awss3.PutObjectOutput{}, nil
		},
		create: func(context.Context, *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
			events = append(events, "create")
			return &awss3.CreateMultipartUploadOutput{UploadId: awssdk.String("id")}, nil
		},
		upload: func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
			events = append(events, "upload")
			return &awss3.UploadPartOutput{ETag: awssdk.String("etag"), ChecksumCRC32: awssdk.String("crc")}, nil
		},
		complete: func(context.Context, *awss3.CompleteMultipartUploadInput) (*awss3.CompleteMultipartUploadOutput, error) {
			events = append(events, "complete")
			return &awss3.CompleteMultipartUploadOutput{}, nil
		},
		abort: func(context.Context, *awss3.AbortMultipartUploadInput) (*awss3.AbortMultipartUploadOutput, error) {
			events = append(events, "abort")
			return &awss3.AbortMultipartUploadOutput{}, nil
		},
	}
	return fake, &events
}

// R-PGNX-9QYI R-VKGA-50DK
func TestPutObjectCancellationFromReadStopsRequests(t *testing.T) {
	for _, offset := range []int{0, PartSize, PartSize + 1, 2*PartSize + 1} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fake, events := failureUploadFake()
			body := &boundaryReader{source: &generatedUpload{left: 3*PartSize + 5}, offset: offset, cancel: cancel, events: events}
			err := newClient(fake, &fakeSSM{}).PutObject(ctx, "s3://bucket/key", body)
			if !errors.Is(err, context.Canceled) || body.readsAfterStop != 0 {
				t.Fatalf("error=%v reads after cancellation=%d events=%v", err, body.readsAfterStop, *events)
			}
			assertStoppedUpload(t, *events, offset)
		})
	}
}

// R-VSD3-F5S3 R-VKGA-50DK R-VO3Z-ABLN
func TestPutObjectMultipartFailureCleanup(t *testing.T) {
	if AbortTimeout != time.Minute {
		t.Fatalf("AbortTimeout=%v", AbortTimeout)
	}
	for _, stage := range []string{"create error", "create nil", "create missing id", "create empty id", "upload error", "upload nil", "upload missing etag", "upload empty etag", "upload missing checksum", "upload empty checksum", "complete error", "create cancellation", "upload cancellation", "complete cancellation"} {
		t.Run(stage, func(t *testing.T) {
			primary := errors.New("primary secret")
			abortErr := errors.New("abort secret")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fake, events := failureUploadFake()
			switch stage {
			case "create error":
				fake.create = func(context.Context, *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
					*events = append(*events, "create")
					return nil, primary
				}
			case "create nil", "create missing id", "create empty id":
				fake.create = func(context.Context, *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
					*events = append(*events, "create")
					if stage == "create nil" {
						return nil, nil
					}
					if stage == "create empty id" {
						return &awss3.CreateMultipartUploadOutput{UploadId: awssdk.String("")}, nil
					}
					return &awss3.CreateMultipartUploadOutput{}, nil
				}
			case "upload error":
				fake.upload = func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
					*events = append(*events, "upload")
					return nil, primary
				}
			case "upload nil", "upload missing etag", "upload empty etag", "upload missing checksum", "upload empty checksum":
				fake.upload = func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
					*events = append(*events, "upload")
					if stage == "upload nil" {
						return nil, nil
					}
					out := &awss3.UploadPartOutput{ETag: awssdk.String("etag"), ChecksumCRC32: awssdk.String("crc")}
					switch stage {
					case "upload missing etag":
						out.ETag = nil
					case "upload empty etag":
						out.ETag = awssdk.String("")
					case "upload missing checksum":
						out.ChecksumCRC32 = nil
					case "upload empty checksum":
						out.ChecksumCRC32 = awssdk.String("")
					}
					return out, nil
				}
			case "complete error":
				fake.complete = func(context.Context, *awss3.CompleteMultipartUploadInput) (*awss3.CompleteMultipartUploadOutput, error) {
					*events = append(*events, "complete")
					return nil, primary
				}
			case "create cancellation":
				old := fake.create
				fake.create = func(ctx context.Context, in *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
					cancel()
					return old(ctx, in)
				}
			case "upload cancellation":
				old := fake.upload
				fake.upload = func(ctx context.Context, in *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
					cancel()
					return old(ctx, in)
				}
			case "complete cancellation":
				old := fake.complete
				fake.complete = func(ctx context.Context, in *awss3.CompleteMultipartUploadInput) (*awss3.CompleteMultipartUploadOutput, error) {
					cancel()
					return old(ctx, in)
				}
			}
			started := time.Now()
			aborts := 0
			fake.abort = func(abortCtx context.Context, in *awss3.AbortMultipartUploadInput) (*awss3.AbortMultipartUploadOutput, error) {
				aborts++
				*events = append(*events, "abort")
				sent := time.Now()
				deadline, ok := abortCtx.Deadline()
				if abortCtx.Err() != nil || !ok || deadline.Before(started.Add(AbortTimeout)) || deadline.After(sent.Add(AbortTimeout)) {
					t.Fatalf("abort context error=%v deadline=%v", abortCtx.Err(), deadline)
				}
				if awssdk.ToString(in.Bucket) != "bucket" || awssdk.ToString(in.Key) != "key" || awssdk.ToString(in.UploadId) != "id" {
					t.Fatalf("abort=%#v", in)
				}
				return nil, abortErr
			}
			err := newClient(fake, &fakeSSM{}).PutObject(ctx, "s3://bucket/key", &generatedUpload{left: PartSize + 1})
			if err == nil || errors.Is(err, abortErr) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v", err)
			}
			if strings.HasSuffix(stage, "error") && !errors.Is(err, primary) {
				t.Fatalf("lost primary error=%v", err)
			}
			if strings.HasSuffix(stage, "cancellation") && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation=%v", err)
			}
			wantAborts := 1
			if strings.HasPrefix(stage, "create ") && stage != "create cancellation" {
				wantAborts = 0
			}
			if aborts != wantAborts {
				t.Fatalf("aborts=%d want=%d events=%v", aborts, wantAborts, *events)
			}
			want := []string{"create"}
			if !strings.HasPrefix(stage, "create ") {
				want = append(want, "upload")
				if strings.HasPrefix(stage, "complete ") {
					want = append(want, "upload", "complete")
				}
			}
			if wantAborts != 0 {
				want = append(want, "abort")
			}
			if strings.Join(*events, ",") != strings.Join(want, ",") {
				t.Fatalf("events=%v want=%v", *events, want)
			}
		})
	}
}

// R-PHVT-NIP7 R-VKGA-50DK
func TestPutObjectMultipartConditionalErrors(t *testing.T) {
	for _, stage := range []string{"create", "upload", "complete", "abort"} {
		for _, conditional := range []struct {
			code      string
			status    int
			collision bool
		}{{"PreconditionFailed", 412, true}, {"ConditionalRequestConflict", 409, true}, {"Other", 412, false}, {"Other", 409, false}, {"PreconditionFailed", 409, false}, {"ConditionalRequestConflict", 412, false}} {
			t.Run(fmt.Sprintf("%s/%s/%d", stage, conditional.code, conditional.status), func(t *testing.T) {
				providerErr := &statusError{status: conditional.status, err: &apiError{code: conditional.code, message: "provider secret"}}
				primary := errors.New("body failure")
				fake, events := failureUploadFake()
				switch stage {
				case "create":
					fake.create = func(context.Context, *awss3.CreateMultipartUploadInput) (*awss3.CreateMultipartUploadOutput, error) {
						*events = append(*events, "create")
						return nil, providerErr
					}
				case "upload":
					fake.upload = func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
						*events = append(*events, "upload")
						return nil, providerErr
					}
				case "complete":
					fake.complete = func(context.Context, *awss3.CompleteMultipartUploadInput) (*awss3.CompleteMultipartUploadOutput, error) {
						*events = append(*events, "complete")
						return nil, providerErr
					}
				case "abort":
					fake.upload = func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
						*events = append(*events, "upload")
						return nil, primary
					}
					fake.abort = func(context.Context, *awss3.AbortMultipartUploadInput) (*awss3.AbortMultipartUploadOutput, error) {
						*events = append(*events, "abort")
						return nil, providerErr
					}
				}
				err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket/key", &generatedUpload{left: PartSize + 1})
				if errors.Is(err, cloud.ErrAlreadyExists) != (stage == "complete" && conditional.collision) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("error=%v events=%v", err, *events)
				}
				cause := error(providerErr)
				if stage == "abort" {
					cause = primary
				}
				if !errors.Is(err, cause) {
					t.Fatalf("cause missing %v", err)
				}
				want := "create"
				if stage == "upload" || stage == "abort" {
					want = "create,upload,abort"
				}
				if stage == "complete" {
					want = "create,upload,upload,complete,abort"
				}
				if strings.Join(*events, ",") != want {
					t.Fatalf("events=%v", *events)
				}
			})
		}
	}
}

// R-PAKF-CW91
func TestPutObjectSingleRequestWaitsForProviderAndReturnsFailure(t *testing.T) {
	failure := errors.New("refused")
	fake := &fakeS3{put: func(context.Context, *awss3.PutObjectInput) (*awss3.PutObjectOutput, error) { return nil, failure }}
	if err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket/key", bytes.NewBufferString("bytes")); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}

// R-PE84-I7H4
func TestPutObjectPreservesErrorsAccompanyingBytes(t *testing.T) {
	for _, afterMultipart := range []bool{false, true} {
		t.Run(fmt.Sprint(afterMultipart), func(t *testing.T) {
			failure := errors.New("source failed")
			fake, events := failureUploadFake()
			body := &bytesAndErrorReader{failure: failure, events: events}
			var source io.Reader = body
			if afterMultipart {
				source = io.MultiReader(&generatedUpload{left: PartSize + 1}, body)
			}
			err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket/key", source)
			if !errors.Is(err, failure) || body.reads != 1 {
				t.Fatalf("error=%v reads=%d events=%v", err, body.reads, *events)
			}
			offset := 0
			if afterMultipart {
				offset = PartSize + 1
			}
			assertStoppedUpload(t, *events, offset)
		})
	}
}

type bytesAndErrorReader struct {
	events  *[]string
	failure error
	reads   int
}

func (r *bytesAndErrorReader) Read(p []byte) (int, error) {
	r.reads++
	*r.events = append(*r.events, "stop")
	return copy(p, "partial"), r.failure
}

// R-PAKF-CW91 R-VR57-1E1E
func TestOpenUploadsUnknownLengthBodiesOverHTTP(t *testing.T) {
	for _, size := range []int{23, PartSize + 7} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var events []string
			var checksums []string
			var total int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/bucket/key" {
					t.Errorf("path=%q", r.URL.Path)
				}
				switch {
				case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
					events = append(events, "create")
					if r.Header.Get("X-Amz-Checksum-Algorithm") != "CRC32" {
						t.Errorf("create algorithm=%q", r.Header.Get("X-Amz-Checksum-Algorithm"))
					}
					_, _ = io.WriteString(w, `<CreateMultipartUploadResult><UploadId>id</UploadId></CreateMultipartUploadResult>`)
				case r.Method == http.MethodPut:
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					for i, b := range data {
						if b != uploadPattern[(total+int64(i))%int64(len(uploadPattern))] {
							t.Errorf("bad upload byte %d", total+int64(i))
							break
						}
					}
					total += int64(len(data))
					var sum [4]byte
					binary.BigEndian.PutUint32(sum[:], crc32.ChecksumIEEE(data))
					checksum := base64.StdEncoding.EncodeToString(sum[:])
					if r.URL.Query().Has("partNumber") {
						events = append(events, "upload")
						if r.URL.Query().Get("uploadId") != "id" || r.Header.Get("X-Amz-Checksum-Crc32") != checksum {
							t.Errorf("part query=%v crc32=%q want=%q", r.URL.Query(), r.Header.Get("X-Amz-Checksum-Crc32"), checksum)
						}
						checksums = append(checksums, checksum)
						w.Header().Set("X-Amz-Checksum-Crc32", checksum)
						w.Header().Set("ETag", fmt.Sprintf("etag-%d", len(checksums)))
					} else {
						events = append(events, "put")
						if r.Header.Get("If-None-Match") != "*" {
							t.Error("missing single condition")
						}
					}
					if r.ContentLength != int64(len(data)) {
						t.Errorf("content length=%d bytes=%d", r.ContentLength, len(data))
					}
				case r.Method == http.MethodPost && r.URL.Query().Has("uploadId"):
					events = append(events, "complete")
					var completion struct {
						Parts []struct {
							Number int    `xml:"PartNumber"`
							ETag   string `xml:"ETag"`
							CRC32  string `xml:"ChecksumCRC32"`
						} `xml:"Part"`
					}
					if err := xml.NewDecoder(r.Body).Decode(&completion); err != nil {
						t.Error(err)
					}
					if r.Header.Get("If-None-Match") != "*" || len(completion.Parts) != len(checksums) {
						t.Errorf("completion=%#v headers=%v", completion, r.Header)
					}
					for i, p := range completion.Parts {
						if p.Number != i+1 || p.ETag != fmt.Sprintf("etag-%d", i+1) || p.CRC32 != checksums[i] {
							t.Errorf("completed part=%#v", p)
						}
					}
					_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>done</ETag></CompleteMultipartUploadResult>`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			configDir := t.TempDir()
			t.Setenv("AWS_CONFIG_FILE", filepath.Join(configDir, "config"))
			t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(configDir, "credentials"))
			t.Setenv("AWS_PROFILE", "")
			t.Setenv("AWS_ENDPOINT_URL_S3", server.URL)
			t.Setenv("AWS_ACCESS_KEY_ID", "test-access")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
			t.Setenv("AWS_SESSION_TOKEN", "")
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			opened, err := Open(t.Context(), "us-east-2")
			if err != nil {
				t.Fatal(err)
			}
			if err = opened.PutObject(t.Context(), "s3://bucket/key", &generatedUpload{left: size}); err != nil {
				t.Fatal(err)
			}
			want := "put"
			if size > PartSize {
				want = "create,upload,upload,complete"
			}
			if strings.Join(events, ",") != want || total != int64(size) {
				t.Fatalf("events=%v total=%d", events, total)
			}
		})
	}
}

// R-VKGA-50DK
func TestPutObjectAbortTimeoutKeepsPrimaryFailure(t *testing.T) {
	primary := errors.New("part failure")
	fake, events := failureUploadFake()
	fake.upload = func(context.Context, *awss3.UploadPartInput) (*awss3.UploadPartOutput, error) {
		*events = append(*events, "upload")
		return nil, primary
	}
	fake.abort = func(context.Context, *awss3.AbortMultipartUploadInput) (*awss3.AbortMultipartUploadOutput, error) {
		*events = append(*events, "abort")
		return nil, context.DeadlineExceeded
	}
	err := newClient(fake, &fakeSSM{}).PutObject(t.Context(), "s3://bucket/key", &generatedUpload{left: PartSize + 1})
	if !errors.Is(err, primary) || errors.Is(err, context.DeadlineExceeded) || strings.Join(*events, ",") != "create,upload,abort" {
		t.Fatalf("error=%v events=%v", err, *events)
	}
}
