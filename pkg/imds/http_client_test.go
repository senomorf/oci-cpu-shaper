package imds_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/imds"
)

const (
	regionResourcePath      = "/opc/v2/instance/region"
	instanceIDResourcePath  = "/opc/v2/instance/id"
	shapeConfigResourcePath = "/opc/v2/instance/shape-config"
)

var (
	errDialFailure = errors.New("dial failure")
	errCloseBoom   = errors.New("close boom")
	errCloseFailed = errors.New("close failure")
)

func TestHTTPClientHappyPath(t *testing.T) {
	t.Parallel()

	region := "us-phoenix-1\n"
	instanceID := "ocid1.instance.oc1..exampleuniqueID"
	shapeBody := `{"ocpus":4,"memoryInGBs":64,` +
		`"baselineOcpuUtilization":"BASELINE_1_1","baselineOcpus":4,` +
		`"threadsPerCore":2,"networkingBandwidthInGbps":10,"maxVnicAttachments":2}`

	responses := map[string]string{
		regionResourcePath:      region,
		instanceIDResourcePath:  instanceID,
		shapeConfigResourcePath: shapeBody,
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			payload, ok := responses[req.URL.Path]
			if !ok {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}

			_, _ = writer.Write([]byte(payload))
		}),
	)
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Timeout = time.Second

	client := imds.NewClient(httpClient, imds.WithBaseURL(server.URL+"/opc/v2"))

	ctx := context.Background()

	gotRegion, err := client.Region(ctx)
	requireNoError(t, err, "Region()")
	requireEqual(t, "Region()", gotRegion, "us-phoenix-1")

	gotID, err := client.InstanceID(ctx)
	requireNoError(t, err, "InstanceID()")
	requireEqual(t, "InstanceID()", gotID, instanceID)

	shapeCfg, err := client.ShapeConfig(ctx)
	requireNoError(t, err, "ShapeConfig()")

	requireEqual(t, "ShapeConfig().OCPUs", shapeCfg.OCPUs, 4.0)
	requireEqual(t, "ShapeConfig().MemoryInGBs", shapeCfg.MemoryInGBs, 64.0)
	requireEqual(t, "ShapeConfig().MaxVnicAttachments", shapeCfg.MaxVnicAttachments, 2)
}

func TestHTTPClientRetriesOnServerError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			if req.URL.Path != regionResourcePath {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}

			if calls.Add(1) == 1 {
				writer.WriteHeader(http.StatusInternalServerError)

				return
			}

			_, _ = writer.Write([]byte("us-ashburn-1"))
		}),
	)
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Timeout = time.Second

	client := imds.NewClient(
		httpClient,
		imds.WithBaseURL(server.URL+"/opc/v2"),
		imds.WithMaxAttempts(3),
		imds.WithBackoff(10*time.Millisecond),
	)

	ctx := context.Background()

	gotRegion, err := client.Region(ctx)
	requireNoError(t, err, "Region()")
	requireEqual(t, "Region()", gotRegion, "us-ashburn-1")
	requireEqual(t, "attempts", calls.Load(), int32(2))
}

func TestHTTPClientRetriesOnTransportError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	httpClient := newHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != regionResourcePath {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}

		switch attempts.Add(1) {
		case 1:
			return nil, errDialFailure
		default:
			return newHTTPResponse(
				http.StatusOK,
				io.NopCloser(strings.NewReader("us-sanjose-1")),
				req,
			), nil
		}
	}))

	client := imds.NewClient(
		httpClient,
		imds.WithBaseURL("http://metadata.local/opc/v2"),
		imds.WithBackoff(5*time.Millisecond),
	)

	ctx := context.Background()

	gotRegion, err := client.Region(ctx)
	requireNoError(t, err, "Region()")
	requireEqual(t, "attempts", attempts.Load(), int32(2))
	requireEqual(t, "Region()", gotRegion, "us-sanjose-1")
}

func TestHTTPClientContextCanceledDuringRequest(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	httpClient := newHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != regionResourcePath {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}

		attempts.Add(1)

		cancelRaw := req.Context().Value(cancelFuncKey{})

		cancel, ok := cancelRaw.(context.CancelFunc)
		if !ok {
			t.Fatalf("missing cancel func in context: %T", cancelRaw)
		}

		cancel()

		return nil, context.Canceled
	}))

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, cancelFuncKey{}, cancel)

	client := imds.NewClient(httpClient, imds.WithBaseURL("http://metadata.local/opc/v2"))

	_, err := client.Region(ctx)
	if err == nil {
		t.Fatalf("Region() expected error, got nil")
	}

	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Region() error = %v, want context canceled", err)
	}

	requireEqual(t, "attempts", attempts.Load(), int32(1))
}

func TestHTTPClientReadFailureIncludesCloseError(t *testing.T) {
	t.Parallel()

	httpClient := newHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newHTTPResponse(
			http.StatusOK,
			&faultyReadCloser{
				readErr:  io.ErrUnexpectedEOF,
				closeErr: errCloseBoom,
			},
			req,
		), nil
	}))

	client := imds.NewClient(httpClient, imds.WithBaseURL("http://metadata.local/opc/v2"))

	_, err := client.Region(context.Background())
	if err == nil {
		t.Fatal("Region() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "read region response") {
		t.Fatalf("Region() error = %v, want read error", err)
	}

	if !strings.Contains(err.Error(), "close response body") {
		t.Fatalf("Region() error = %v, want close error joined", err)
	}
}

func TestHTTPClientCloseFailure(t *testing.T) {
	t.Parallel()

	httpClient := newHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newHTTPResponse(
			http.StatusOK,
			&staticBody{
				data:     []byte("us-london-1"),
				once:     sync.Once{},
				closeErr: errCloseFailed,
			},
			req,
		), nil
	}))

	client := imds.NewClient(httpClient, imds.WithBaseURL("http://metadata.local/opc/v2"))

	_, err := client.Region(context.Background())
	if err == nil {
		t.Fatal("Region() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "close region response body") {
		t.Fatalf("Region() error = %v, want close failure", err)
	}
}

func TestHTTPClientNonRetryableStatusIncludesBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(" not found \n"))
	}))
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Timeout = time.Second

	client := imds.NewClient(httpClient, imds.WithBaseURL(server.URL+"/opc/v2"))

	_, err := client.Region(context.Background())
	if err == nil {
		t.Fatal("Region() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "body not found") {
		t.Fatalf("Region() error = %v, want trimmed body", err)
	}
}

func TestHTTPClientRetryBudgetExhaustedIncludesLastError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Timeout = time.Second

	client := imds.NewClient(
		httpClient,
		imds.WithBaseURL(server.URL+"/opc/v2"),
		imds.WithMaxAttempts(2),
		imds.WithBackoff(10*time.Millisecond),
	)

	_, err := client.Region(context.Background())
	if err == nil {
		t.Fatal("Region() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "exhausted retry budget") {
		t.Fatalf("Region() error = %v, want exhausted retry budget", err)
	}

	if !strings.Contains(err.Error(), "retryable status code") {
		t.Fatalf("Region() error = %v, want last retryable status code", err)
	}

	requireEqual(t, "attempts", attempts.Load(), int32(2))
}

func TestHTTPClientWaitHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	attemptCh := make(chan struct{})
	doneCh := make(chan struct{})

	httpClient := newHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case attemptCh <- struct{}{}:
		default:
		}

		return newHTTPResponse(
			http.StatusServiceUnavailable,
			io.NopCloser(strings.NewReader("retry later")),
			req,
		), nil
	}))

	client := imds.NewClient(
		httpClient,
		imds.WithBaseURL("http://metadata.local/opc/v2"),
		imds.WithMaxAttempts(2),
		imds.WithBackoff(250*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer close(doneCh)

		_, _ = client.Region(ctx)
	}()

	<-attemptCh
	cancel()

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("Region() did not return after context cancellation")
	}
}

func TestShapeConfigDecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != shapeConfigResourcePath {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}

		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Timeout = time.Second

	client := imds.NewClient(httpClient, imds.WithBaseURL(server.URL+"/opc/v2"))

	_, err := client.ShapeConfig(context.Background())
	if err == nil {
		t.Fatal("ShapeConfig() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "decode shape-config response") {
		t.Fatalf("ShapeConfig() error = %v, want decode failure", err)
	}
}

func requireNoError(t *testing.T, err error, msg string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

func requireEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type cancelFuncKey struct{}

type faultyReadCloser struct {
	readErr  error
	closeErr error
}

func (f *faultyReadCloser) Read(_ []byte) (int, error) {
	return 0, f.readErr
}

func (f *faultyReadCloser) Close() error {
	return f.closeErr
}

type staticBody struct {
	data     []byte
	once     sync.Once
	closeErr error
}

func (s *staticBody) Read(p []byte) (int, error) {
	var bytesCopied int

	s.once.Do(func() {
		bytesCopied = copy(p, s.data)
	})

	if bytesCopied == 0 {
		return 0, io.EOF
	}

	return bytesCopied, io.EOF
}

func (s *staticBody) Close() error {
	return s.closeErr
}

func newHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport:     transport,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
}

func newHTTPResponse(statusCode int, body io.ReadCloser, req *http.Request) *http.Response {
	return &http.Response{
		Status:           fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		StatusCode:       statusCode,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           make(http.Header),
		Body:             body,
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          req,
		TLS:              nil,
	}
}
