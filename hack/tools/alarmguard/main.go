package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

const (
	defaultTimeout         = 60 * time.Second
	defaultPendingDuration = "PT1H"
	defaultResolution      = "1m"
	listPageLimit          = 1000

	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var (
	errCompartmentRequired = errors.New("compartment OCID is required")
	errInstanceRequired    = errors.New("instance OCID is required")
	errRegionRequired      = errors.New("region is required")
	errTimeoutInvalid      = errors.New("timeout must be greater than zero")
	errGuardrailMissing    = errors.New(
		"no Always Free P95 alarm matched the expected configuration",
	)
)

type config struct {
	CompartmentID       string
	MetricCompartmentID string
	InstanceID          string
	Region              string
	RequireDestinations bool
	Timeout             time.Duration
	ExpectedPending     string
	ExpectedResolution  string
}

func main() {
	if code := run(os.Args[1:]); code != exitOK {
		os.Exit(code)
	}
}

func run(args []string) int {
	cfg, err := parseConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "alarmguard: %v\n", err)

		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	provider, err := auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"alarmguard: failed to initialise instance principal provider: %v\n",
			err,
		)

		return exitError
	}

	client, err := monitoring.NewMonitoringClientWithConfigurationProvider(provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "alarmguard: failed to create monitoring client: %v\n", err)

		return exitError
	}

	client.SetRegion(cfg.Region)

	guardPresent, err := findGuardrail(ctx, client, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "alarmguard: %v\n", err)

		return exitError
	}

	if !guardPresent {
		fmt.Fprintf(os.Stderr, "alarmguard: %v\n", errGuardrailMissing)

		return exitError
	}

	return exitOK
}

func parseConfig(args []string) (config, error) {
	cfg := config{ //nolint:exhaustruct
		RequireDestinations: true,
		Timeout:             defaultTimeout,
		ExpectedPending:     defaultPendingDuration,
		ExpectedResolution:  defaultResolution,
	}

	var metricCompartment string

	flagSet := flag.NewFlagSet("alarmguard", flag.ContinueOnError)
	registerFlags(flagSet, &cfg, &metricCompartment)

	err := flagSet.Parse(args)
	if err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}

	if metricCompartment != "" {
		cfg.MetricCompartmentID = metricCompartment
	}

	err = cfg.validate()
	if err != nil {
		return config{}, err
	}

	return cfg, nil
}

func (c config) validate() error {
	switch {
	case c.CompartmentID == "":
		return errCompartmentRequired
	case c.InstanceID == "":
		return errInstanceRequired
	case c.Region == "":
		return errRegionRequired
	case c.Timeout <= 0:
		return errTimeoutInvalid
	default:
		return nil
	}
}

func findGuardrail(ctx context.Context, client monitoringClient, cfg config) (bool, error) {
	request := monitoring.ListAlarmsRequest{ //nolint:exhaustruct
		CompartmentId:  common.String(cfg.CompartmentID),
		LifecycleState: monitoring.AlarmLifecycleStateActive,
		Limit:          common.Int(listPageLimit),
	}

	for {
		response, err := client.ListAlarms(ctx, request)
		if err != nil {
			return false, fmt.Errorf("list alarms: %w", err)
		}

		for _, summary := range response.Items {
			if !summaryMatches(summary, cfg) {
				continue
			}

			detailResponse, err := client.GetAlarm(
				ctx,
				monitoring.GetAlarmRequest{ //nolint:exhaustruct
					AlarmId: summary.Id,
				},
			)
			if err != nil {
				return false, fmt.Errorf("get alarm %s: %w", stringValue(summary.Id), err)
			}

			if detailMatches(summary, detailResponse.Alarm, cfg) {
				return true, nil
			}
		}

		if response.OpcNextPage == nil || len(*response.OpcNextPage) == 0 {
			break
		}

		request.Page = response.OpcNextPage
	}

	return false, nil
}

func summaryMatches(summary monitoring.AlarmSummary, cfg config) bool {
	if summary.LifecycleState != monitoring.AlarmLifecycleStateActive {
		return false
	}

	if summary.IsEnabled == nil || !*summary.IsEnabled {
		return false
	}

	if cfg.RequireDestinations && len(summary.Destinations) == 0 {
		return false
	}

	if !namespaceMatches(summary.Namespace) {
		return false
	}

	return queryMatches(stringValue(summary.Query), cfg.InstanceID)
}

func detailMatches(summary monitoring.AlarmSummary, detail monitoring.Alarm, cfg config) bool {
	if !optionalNamespaceMatches(detail.Namespace) {
		return false
	}

	query := stringValue(detail.Query)
	if query == "" {
		query = stringValue(summary.Query)
	}

	if !queryMatches(query, cfg.InstanceID) {
		return false
	}

	if !metricCompartmentMatches(detail.MetricCompartmentId, cfg.MetricCompartmentID) {
		return false
	}

	if !durationMatches(detail.PendingDuration, cfg.ExpectedPending) {
		return false
	}

	return resolutionMatches(detail.Resolution, cfg.ExpectedResolution)
}

func namespaceMatches(ptr *string) bool {
	return strings.ToLower(stringValue(ptr)) == "oci_computeagent"
}

func optionalNamespaceMatches(ptr *string) bool {
	if ptr == nil {
		return true
	}

	return namespaceMatches(ptr)
}

func metricCompartmentMatches(actual *string, expected string) bool {
	if expected == "" {
		return true
	}

	return stringValue(actual) == expected
}

func durationMatches(actual *string, expected string) bool {
	if expected == "" {
		return true
	}

	if actual == nil {
		return false
	}

	return strings.EqualFold(*actual, expected)
}

func resolutionMatches(actual *string, expected string) bool {
	if expected == "" {
		return true
	}

	if actual == nil {
		return false
	}

	return strings.EqualFold(*actual, expected)
}

func queryMatches(query, instanceID string) bool {
	if query == "" {
		return false
	}

	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(query, " ", ""), "\n", ""))
	expectedResource := fmt.Sprintf("resourceid=\"%s\"", strings.ToLower(instanceID))

	if !strings.Contains(normalized, "cpuutilization[1m]{") {
		return false
	}

	if !strings.Contains(normalized, expectedResource) {
		return false
	}

	if !strings.Contains(normalized, ".window(7d).") {
		return false
	}

	if !strings.Contains(normalized, ".percentile(0.95)") {
		return false
	}

	return strings.Contains(normalized, "<20")
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}

	return *ptr
}

type monitoringClient interface {
	ListAlarms(
		ctx context.Context,
		request monitoring.ListAlarmsRequest,
	) (monitoring.ListAlarmsResponse, error)
	GetAlarm(
		ctx context.Context,
		request monitoring.GetAlarmRequest,
	) (monitoring.GetAlarmResponse, error)
}

func registerFlags(flagSet *flag.FlagSet, cfg *config, metricCompartment *string) {
	flagSet.SetOutput(os.Stderr)
	flagSet.StringVar(
		&cfg.CompartmentID,
		"compartment",
		"",
		"Compartment OCID that should contain the guardrail alarm.",
	)
	flagSet.StringVar(
		metricCompartment,
		"metric-compartment",
		"",
		"Optional compartment OCID for the alarm's metric scope (defaults to skipping the check).",
	)
	flagSet.StringVar(
		&cfg.InstanceID,
		"instance",
		"",
		"Instance OCID protected by the Always Free guardrail.",
	)
	flagSet.StringVar(
		&cfg.Region,
		"region",
		"",
		"OCI region identifier (for example, us-phoenix-1).",
	)
	flagSet.DurationVar(
		&cfg.Timeout,
		"timeout",
		defaultTimeout,
		"Overall timeout for the alarm verification call.",
	)
	flagSet.BoolVar(
		&cfg.RequireDestinations,
		"require-destinations",
		true,
		"Fail when the guardrail alarm does not target any notification destinations.",
	)
	flagSet.StringVar(
		&cfg.ExpectedPending,
		"expected-pending",
		defaultPendingDuration,
		"Expected ISO-8601 pending duration for the guardrail alarm.",
	)
	flagSet.StringVar(
		&cfg.ExpectedResolution,
		"expected-resolution",
		defaultResolution,
		"Expected monitoring resolution for the guardrail alarm.",
	)
}
