package oci

import "context"

// NewStaticMetricsClient returns a MetricsClient that always reports the provided value.
//
// Tests and CLI wiring still depend on the interface return type.
//

func NewStaticMetricsClient(value float64) MetricsClient {
	return &staticMetricsClient{value: value}
}

type staticMetricsClient struct {
	value float64
}

func (c *staticMetricsClient) QueryP95CPU(context.Context, string) (float64, error) {
	return c.value, nil
}
