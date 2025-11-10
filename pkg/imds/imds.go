// Package imds contains clients for the OCI Instance Metadata Service (IMDSv2).
package imds

import "context"

// Client describes the metadata operations needed by the CPU shaper.
type Client interface {
	// Region returns the canonical region for the running instance.
	Region(ctx context.Context) (string, error)
	// InstanceID returns the OCID of the running instance.
	InstanceID(ctx context.Context) (string, error)
}

// DummyClient is a placeholder IMDS implementation used during bootstrap.
type DummyClient struct{}

// NewDummyClient returns a Client that supplies deterministic dummy values.
func NewDummyClient() Client {
	return DummyClient{}
}

// Region returns a synthetic region identifier for development use.
func (DummyClient) Region(ctx context.Context) (string, error) {
	return "dummy-region-1", nil
}

// InstanceID returns a placeholder instance OCID.
func (DummyClient) InstanceID(ctx context.Context) (string, error) {
	return "ocid1.instance.oc1..dummy", nil
}
