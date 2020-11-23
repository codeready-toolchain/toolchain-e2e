package testsupport

import (
	"time"
)

// MetricsAssertionHelper stores baseline metric values when initialized and has convenient functions for metrics assertions
type MetricsAssertionHelper struct {
	await          metricsProvider
	baselineValues map[string]float64
}

type metricKey string

type metricsProvider interface {
	GetMetricValue(family string, labels ...string) float64
	WaitForUserSignupsBeingDeleted(initialDelay time.Duration) error
	AssertMetricReachesValue(family string, expectedValue float64, labels ...string)
}

// metric constants
const (
	UserSignupsMetric                = "sandbox_user_signups_total"
	UserSignupsApprovedMetric        = "sandbox_user_signups_approved_total"
	CurrentMURsMetric                = "sandbox_master_user_record_current"
	UserSignupsDeactivatedMetric     = "sandbox_user_signups_deactivated_total"
	UserSignupsAutoDeactivatedMetric = "sandbox_user_signups_auto_deactivated_total"
	UserSignupsBannedMetric          = "sandbox_user_signups_banned_total"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func InitMetricsAssertion(a metricsProvider) *MetricsAssertionHelper {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	a.WaitForUserSignupsBeingDeleted(5 * time.Second)

	// Capture baseline values
	m := &MetricsAssertionHelper{
		await:          a,
		baselineValues: make(map[string]float64),
	}
	m.captureBaselineValues()
	return m
}

func (m *MetricsAssertionHelper) captureBaselineValues() {
	m.baselineValues[UserSignupsMetric] = m.await.GetMetricValue(UserSignupsMetric)
	m.baselineValues[UserSignupsApprovedMetric] = m.await.GetMetricValue(UserSignupsApprovedMetric)
	m.baselineValues[CurrentMURsMetric] = m.await.GetMetricValue(CurrentMURsMetric)
	m.baselineValues[UserSignupsDeactivatedMetric] = m.await.GetMetricValue(UserSignupsDeactivatedMetric)
	m.baselineValues[UserSignupsAutoDeactivatedMetric] = m.await.GetMetricValue(UserSignupsAutoDeactivatedMetric)
}

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (m *MetricsAssertionHelper) WaitForMetricDelta(family metricKey, delta float64) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := string(family)
	adjustedValue := m.baselineValues[key] + delta
	m.await.AssertMetricReachesValue(key, adjustedValue)
}
