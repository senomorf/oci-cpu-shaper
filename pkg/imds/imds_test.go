package imds_test

//nolint:depguard // IMDS tests import the package under test
import (
	"testing"

	"oci-cpu-shaper/pkg/imds"
)

func TestDummyClientProvidesDeterministicValues(t *testing.T) {
	t.Parallel()

	client := imds.NewDummyClient()

	region, err := client.Region(t.Context())
	if err != nil {
		t.Fatalf("DummyClient.Region returned error: %v", err)
	}

	if region != "dummy-region-1" {
		t.Fatalf("unexpected region value: %q", region)
	}

	instanceID, err := client.InstanceID(t.Context())
	if err != nil {
		t.Fatalf("DummyClient.InstanceID returned error: %v", err)
	}

	if instanceID != "ocid1.instance.oc1..dummy" {
		t.Fatalf("unexpected instance ID: %q", instanceID)
	}
}
