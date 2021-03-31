package testsupport

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// MetricsAssertionHelper stores baseline metric values when initialized and has convenient functions for metrics assertions
type MetricsAssertionHelper struct {
	await          metricsProvider
	baselineValues map[string]float64
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

	// DEPRECATED: use `UserAccountsMetric` instead
	MasterUserRecordMetric = "sandbox_master_user_record_current"
	UserAccountsMetric     = "sandbox_user_accounts_current"

	ActivationsPerUserMetric = "sandbox_activations_per_user"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func InitMetricsAssertion(t *testing.T, a metricsProvider, memberClusterNames []string) *MetricsAssertionHelper {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	err := a.WaitForTestResourcesCleanup(5 * time.Second)
	require.NoError(t, err)

	// Capture baseline values
	m := &MetricsAssertionHelper{
		await:          a,
		baselineValues: make(map[string]float64),
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
	for _, name := range memberClusterNames { // sum of gauge value of all member clusters
		key := baselineKey(UserAccountsMetric, "cluster_name", name)
		m.baselineValues[key] += m.await.GetMetricValue(UserAccountsMetric, "cluster_name", name)
	}
}

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (m *MetricsAssertionHelper) WaitForMetricDelta(family string, delta float64, labels ...string) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := baselineKey(string(family), labels...)
	adjustedValue := m.baselineValues[key] + delta
	m.await.AssertMetricReachesValue(string(family), adjustedValue, labels...)
}

// generates a key to retain the baseline metric value, by joining the metric name and its labels.
// Note: there are probably more sophisticated ways to combine the name and the labels, but for now
// this simple concatenation should be enough to make the keys unique
func baselineKey(name string, labels ...string) string {
	return strings.Join(append([]string{name}, labels...), ",")
}
