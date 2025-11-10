package imds_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/imds"
)

func TestHTTPClientHappyPath(t *testing.T) {
	t.Parallel()

	region := "us-phoenix-1\n"
	instanceID := "ocid1.instance.oc1..exampleuniqueID"
	shapeBody := `{"ocpus":4,"memoryInGBs":64,` +
		`"baselineOcpuUtilization":"BASELINE_1_1","baselineOcpus":4,` +
		`"threadsPerCore":2,"networkingBandwidthInGbps":10,"maxVnicAttachments":2}`

	responses := map[string]string{
		"/opc/v2/instance/region":       region,
		"/opc/v2/instance/id":           instanceID,
		"/opc/v2/instance/shape-config": shapeBody,
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
			if req.URL.Path != "/opc/v2/instance/region" {
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
