package imds

import (
	"context"
	"testing"
)

func TestDummyClientProvidesDeterministicValues(t *testing.T) {
	t.Parallel()

	client := NewDummyClient()

	region, err := client.Region(context.Background())
	if err != nil {
		t.Fatalf("DummyClient.Region returned error: %v", err)
	}
	if region != "dummy-region-1" {
		t.Fatalf("unexpected region value: %q", region)
	}

	instanceID, err := client.InstanceID(context.Background())
	if err != nil {
		t.Fatalf("DummyClient.InstanceID returned error: %v", err)
	}
	if instanceID != "ocid1.instance.oc1..dummy" {
		t.Fatalf("unexpected instance ID: %q", instanceID)
	}
}
