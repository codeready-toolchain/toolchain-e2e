package testsupport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// MetricsAssertionHelper stores baseline metric values when initialized and has convenient functions for metrics assertions
type MetricsAssertionHelper struct {
	await                 metricsProvider
	baselineValues        map[string]float64
	baselineLabeledValues map[string]map[string]float64
}

type metricsProvider interface {
	GetMetricValue(family string, labels ...string) float64
	WaitForTestResourcesCleanup(initialDelay time.Duration) error
	AssertMetricReachesValue(family string, expectedValue float64, labels ...string)
}

// metric constants
const (
	UserSignupsMetric                = "sandbox_user_signups_total"
	UserSignupsApprovedMetric        = "sandbox_user_signups_approved_total"
	UserSignupsDeactivatedMetric     = "sandbox_user_signups_deactivated_total"
	UserSignupsAutoDeactivatedMetric = "sandbox_user_signups_auto_deactivated_total"
	UserSignupsBannedMetric          = "sandbox_user_signups_banned_total"

	// DEPRECATED: use `MasterUserRecordsMetric` instead
	MasterUserRecordMetric  = "sandbox_master_user_record_current"
	MasterUserRecordsMetric = "sandbox_master_user_records_current"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func InitMetricsAssertion(t *testing.T, a metricsProvider, memberClusterNames ...string) *MetricsAssertionHelper {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	err := a.WaitForTestResourcesCleanup(5 * time.Second)
	require.NoError(t, err)

	// Capture baseline values
	m := &MetricsAssertionHelper{
		await:                 a,
		baselineValues:        make(map[string]float64),
		baselineLabeledValues: make(map[string]map[string]float64),
	}
	m.captureBaselineValues(memberClusterNames)
	return m
}

func (m *MetricsAssertionHelper) captureBaselineValues(memberClusterNames []string) {
	m.baselineValues[UserSignupsMetric] = m.await.GetMetricValue(UserSignupsMetric)
	m.baselineValues[UserSignupsApprovedMetric] = m.await.GetMetricValue(UserSignupsApprovedMetric)
	m.baselineValues[UserSignupsDeactivatedMetric] = m.await.GetMetricValue(UserSignupsDeactivatedMetric)
	m.baselineValues[UserSignupsAutoDeactivatedMetric] = m.await.GetMetricValue(UserSignupsAutoDeactivatedMetric)
	m.baselineValues[UserSignupsBannedMetric] = m.await.GetMetricValue(UserSignupsBannedMetric)
	m.baselineValues[MasterUserRecordMetric] = m.await.GetMetricValue(MasterUserRecordMetric)
	m.baselineLabeledValues[MasterUserRecordsMetric] = make(map[string]float64, len(memberClusterNames))
	for _, name := range memberClusterNames {
		m.baselineLabeledValues[MasterUserRecordsMetric][name] = m.await.GetMetricValue(MasterUserRecordsMetric, "cluster_name", name)
	}
}

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (m *MetricsAssertionHelper) WaitForMetricDelta(family string, delta float64, labels ...string) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := string(family)
	adjustedValue := m.baselineValues[key] + delta
	m.await.AssertMetricReachesValue(key, adjustedValue, labels...)
}
