package main

import (
	"context"
	"errors"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

const guardrailQuery = "CpuUtilization[1m]{resourceId=\"ocid1.instance.oc1..guard\"}.window(7d).percentile(0.95) < 20"

var (
	errListNotImplemented = errors.New("list not implemented")
	errGetNotImplemented  = errors.New("get not implemented")
	errUnexpectedGet      = errors.New("unexpected get")
)

type fakeClient struct {
	listFn func(context.Context, monitoring.ListAlarmsRequest) (monitoring.ListAlarmsResponse, error)
	getFn  func(context.Context, monitoring.GetAlarmRequest) (monitoring.GetAlarmResponse, error)
}

func (f fakeClient) ListAlarms(
	ctx context.Context,
	req monitoring.ListAlarmsRequest,
) (monitoring.ListAlarmsResponse, error) {
	if f.listFn == nil {
		return monitoring.ListAlarmsResponse{}, errListNotImplemented
	}

	return f.listFn(ctx, req)
}

func (f fakeClient) GetAlarm(
	ctx context.Context,
	req monitoring.GetAlarmRequest,
) (monitoring.GetAlarmResponse, error) {
	if f.getFn == nil {
		return monitoring.GetAlarmResponse{}, errGetNotImplemented
	}

	return f.getFn(ctx, req)
}

func TestQueryMatches(t *testing.T) {
	t.Parallel()

	instance := "ocid1.instance.oc1..example"
	valid := "CpuUtilization[1m]{resourceId=\"ocid1.instance.oc1..example\"}.window(7d).percentile(0.95) < 20"

	if !queryMatches(valid, instance) {
		t.Fatalf("expected valid guardrail query to match")
	}

	missingWindow := "CpuUtilization[1m]{resourceId=\"ocid1.instance.oc1..example\"}.percentile(0.95) < 20"
	if queryMatches(missingWindow, instance) {
		t.Fatalf("expected query without window to fail")
	}

	wrongInstance := "CpuUtilization[1m]{resourceId=\"ocid1.instance.oc1..other\"}.window(7d).percentile(0.95) < 20"
	if queryMatches(wrongInstance, instance) {
		t.Fatalf("expected query with different resourceId to fail")
	}
}

func TestSummaryAndDetailMatches(t *testing.T) {
	t.Parallel()

	summary := monitoring.AlarmSummary{ //nolint:exhaustruct
		Id:             common.String("ocid1.alarm.oc1..summary"),
		LifecycleState: monitoring.AlarmLifecycleStateActive,
		IsEnabled:      common.Bool(true),
		Namespace:      common.String("oci_computeagent"),
		Destinations:   []string{"ocid1.topic.oc1..dest"},
		Query:          common.String(guardrailQuery),
	}

	detail := monitoring.Alarm{ //nolint:exhaustruct
		Namespace:           common.String("oci_computeagent"),
		Query:               common.String(guardrailQuery),
		MetricCompartmentId: common.String("ocid1.compartment.oc1..metrics"),
		PendingDuration:     common.String("PT1H"),
		Resolution:          common.String("1m"),
	}

	cfg := config{ //nolint:exhaustruct
		InstanceID:          "ocid1.instance.oc1..guard",
		MetricCompartmentID: "ocid1.compartment.oc1..metrics",
		RequireDestinations: true,
		ExpectedPending:     "PT1H",
		ExpectedResolution:  "1m",
	}

	if !summaryMatches(summary, cfg) {
		t.Fatalf("expected summary to match guardrail requirements")
	}

	if !detailMatches(summary, detail, cfg) {
		t.Fatalf("expected detail to match guardrail requirements")
	}

	detail.PendingDuration = common.String("PT5M")
	if detailMatches(summary, detail, cfg) {
		t.Fatalf("expected pending duration mismatch to fail the guard")
	}
}

func TestFindGuardrail(t *testing.T) {
	t.Parallel()

	summary, detail, cfg := guardrailFixtures()

	t.Run("match", func(t *testing.T) {
		t.Parallel()

		client := fakeClient{
			listFn: func(_ context.Context, req monitoring.ListAlarmsRequest) (monitoring.ListAlarmsResponse, error) {
				if stringValue(req.CompartmentId) != cfg.CompartmentID {
					t.Fatalf("unexpected compartment id: %s", stringValue(req.CompartmentId))
				}

				resp := monitoring.ListAlarmsResponse{ //nolint:exhaustruct
					Items: []monitoring.AlarmSummary{summary},
				}

				return resp, nil
			},
			getFn: func(_ context.Context, req monitoring.GetAlarmRequest) (monitoring.GetAlarmResponse, error) {
				if stringValue(req.AlarmId) != stringValue(summary.Id) {
					t.Fatalf("unexpected alarm id lookup: %s", stringValue(req.AlarmId))
				}

				return monitoring.GetAlarmResponse{Alarm: detail}, nil //nolint:exhaustruct
			},
		}

		matched, err := findGuardrail(context.Background(), client, cfg)
		if err != nil {
			t.Fatalf("findGuardrail returned error: %v", err)
		}

		if !matched {
			t.Fatalf("expected guardrail to be detected")
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		client := fakeClient{
			listFn: func(_ context.Context, _ monitoring.ListAlarmsRequest) (monitoring.ListAlarmsResponse, error) {
				resp := monitoring.ListAlarmsResponse{ //nolint:exhaustruct
					Items: []monitoring.AlarmSummary{},
				}

				return resp, nil
			},
			getFn: func(_ context.Context, _ monitoring.GetAlarmRequest) (monitoring.GetAlarmResponse, error) {
				return monitoring.GetAlarmResponse{}, errUnexpectedGet
			},
		}

		matched, err := findGuardrail(context.Background(), client, cfg)
		if err != nil {
			t.Fatalf("findGuardrail returned error with empty list: %v", err)
		}

		if matched {
			t.Fatalf("expected guardrail to be absent")
		}
	})
}

func guardrailFixtures() (monitoring.AlarmSummary, monitoring.Alarm, config) {
	summary := monitoring.AlarmSummary{ //nolint:exhaustruct
		Id:             common.String("ocid1.alarm.oc1..guard"),
		LifecycleState: monitoring.AlarmLifecycleStateActive,
		IsEnabled:      common.Bool(true),
		Namespace:      common.String("oci_computeagent"),
		Destinations:   []string{"ocid1.topic.oc1..dest"},
		Query:          common.String(guardrailQuery),
	}

	detail := monitoring.Alarm{ //nolint:exhaustruct
		Namespace:           common.String("oci_computeagent"),
		Query:               summary.Query,
		MetricCompartmentId: common.String("ocid1.compartment.oc1..metrics"),
		PendingDuration:     common.String("PT1H"),
		Resolution:          common.String("1m"),
	}

	cfg := config{ //nolint:exhaustruct
		CompartmentID:       "ocid1.compartment.oc1..root",
		InstanceID:          "ocid1.instance.oc1..guard",
		MetricCompartmentID: "ocid1.compartment.oc1..metrics",
		RequireDestinations: true,
		ExpectedPending:     "PT1H",
		ExpectedResolution:  "1m",
	}

	return summary, detail, cfg
}
