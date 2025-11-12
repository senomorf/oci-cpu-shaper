package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"oci-cpu-shaper/pkg/oci"
)

const defaultTimeout = 30 * time.Second

var (
	errMissingInstance    = errors.New("instance OCID is required")
	errMissingCompartment = errors.New("compartment OCID is required")
)

type queryConfig struct {
	instanceID    string
	compartmentID string
	region        string
	last7d        bool
	timeout       time.Duration
	allowEmpty    bool
}

func main() {
	cfg := parseConfig()

	err := runQuery(cfg)
	if err != nil {
		logFatal(err)
	}
}

func parseConfig() queryConfig {
	var cfg queryConfig

	flag.StringVar(&cfg.instanceID, "instance", "", "OCID of the compute instance to query")
	flag.StringVar(
		&cfg.compartmentID,
		"compartment",
		"",
		"Compartment OCID scoped for Monitoring queries",
	)
	flag.StringVar(&cfg.region, "region", "", "OCI region override (optional)")
	flag.BoolVar(
		&cfg.last7d,
		"last7d",
		true,
		"Query the trailing seven days instead of the last 24 hours",
	)
	flag.DurationVar(
		&cfg.timeout,
		"timeout",
		defaultTimeout,
		"Timeout for the Monitoring API request",
	)
	flag.BoolVar(
		&cfg.allowEmpty,
		"allow-empty",
		false,
		"Exit successfully when Monitoring returns no datapoints",
	)

	flag.Parse()

	return cfg
}

func runQuery(cfg queryConfig) error {
	if cfg.instanceID == "" {
		return errMissingInstance
	}

	if cfg.compartmentID == "" {
		return errMissingCompartment
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	client, err := oci.NewInstancePrincipalClient(cfg.compartmentID, cfg.region)
	if err != nil {
		return fmt.Errorf("build instance principal client: %w", err)
	}

	value, err := client.QueryP95CPU(ctx, cfg.instanceID, cfg.last7d)
	if err != nil {
		if errors.Is(err, oci.ErrNoMetricsData) && cfg.allowEmpty {
			log.Printf("no metrics returned for %s", cfg.instanceID)

			return nil
		}

		return fmt.Errorf("query P95 CPU: %w", err)
	}

	log.Printf("P95 CPU utilisation for %s: %.2f%%", cfg.instanceID, value)

	return nil
}

func logFatal(err error) {
	log.Printf("error: %v", err)
	os.Exit(1)
}
