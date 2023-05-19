package testsupport

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
)

// MetricsAssertionHelper stores baseline metric values when initialized and has convenient functions for metrics assertions
type MetricsAssertionHelper struct {
	await          metricsProvider
	baselineValues map[string]float64
}

type metricsProvider interface {
	GetMetricValue(t *testing.T, family string, labels ...string) float64
	GetMetricValueOrZero(t *testing.T, family string, labels ...string) float64
	WaitForTestResourcesCleanup(t *testing.T, initialDelay time.Duration) error
	WaitUntiltMetricHasValue(t *testing.T, family string, expectedValue float64, labels ...string)
}

// metric constants
const (
	UserSignupsMetric                    = "sandbox_user_signups_total"
	UserSignupsApprovedMetric            = "sandbox_user_signups_approved_total"
	UserSignupsApprovedWithMethodMetric  = "sandbox_user_signups_approved_with_method_total"
	UserSignupsDeactivatedMetric         = "sandbox_user_signups_deactivated_total"
	UserSignupsAutoDeactivatedMetric     = "sandbox_user_signups_auto_deactivated_total"
	UserSignupsBannedMetric              = "sandbox_user_signups_banned_total"
	UserSignupVerificationRequiredMetric = "sandbox_user_signups_verification_required_total"

	MasterUserRecordsPerDomainMetric = "sandbox_master_user_records"

	SpacesMetric = "sandbox_spaces_current"

	UsersPerActivationsAndDomainMetric = "sandbox_users_per_activations_and_domain"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func InitMetricsAssertion(t *testing.T, awaitilities wait.Awaitilities) *MetricsAssertionHelper {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	err := awaitilities.Host().WaitForTestResourcesCleanup(t, 10*time.Second)
	require.NoError(t, err)
	// wait for toolchainstatus metrics to be updated
	_, err = awaitilities.Host().WaitForToolchainStatus(t,
		wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		wait.UntilToolchainStatusUpdatedAfter(time.Now()))
	require.NoError(t, err)

	// Capture baseline values
	m := &MetricsAssertionHelper{
		await:          awaitilities.Host(),
		baselineValues: make(map[string]float64),
	}
	m.captureBaselineValues(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Logf("captured baselines:\n%s", spew.Sdump(m.baselineValues))
	return m
}

func (m *MetricsAssertionHelper) captureBaselineValues(t *testing.T, memberClusterNames ...string) {
	m.baselineValues[UserSignupsMetric] = m.await.GetMetricValue(t, UserSignupsMetric)
	m.baselineValues[UserSignupsApprovedMetric] = m.await.GetMetricValue(t, UserSignupsApprovedMetric)
	m.baselineValues[UserSignupsDeactivatedMetric] = m.await.GetMetricValue(t, UserSignupsDeactivatedMetric)
	m.baselineValues[UserSignupsAutoDeactivatedMetric] = m.await.GetMetricValue(t, UserSignupsAutoDeactivatedMetric)
	m.baselineValues[UserSignupsBannedMetric] = m.await.GetMetricValue(t, UserSignupsBannedMetric)
	m.baselineValues[UserSignupVerificationRequiredMetric] = m.await.GetMetricValue(t, UserSignupVerificationRequiredMetric)
	for _, name := range memberClusterNames { // sum of gauge value of all member clusters
		spacesKey := m.baselineKey(t, SpacesMetric, "cluster_name", name)
		m.baselineValues[spacesKey] += m.await.GetMetricValue(t, SpacesMetric, "cluster_name", name)
	}
	// capture `sandbox_users_per_activations_and_domain` with "activations" from `1` to `10` and `internal`/`external` domains
	for i := 1; i <= 10; i++ {
		for _, domain := range []string{"internal", "external"} {
			key := m.baselineKey(t, UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
			m.baselineValues[key] = m.await.GetMetricValueOrZero(t, UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
		}
	}
	for _, domain := range []string{"internal", "external"} {
		key := m.baselineKey(t, MasterUserRecordsPerDomainMetric, "domain", domain)
		m.baselineValues[key] = m.await.GetMetricValueOrZero(t, MasterUserRecordsPerDomainMetric, "domain", domain)
	}
	for _, approvalMethod := range []string{"automatic", "manual"} {
		key := m.baselineKey(t, UserSignupsApprovedWithMethodMetric, "method", approvalMethod)
		m.baselineValues[key] = m.await.GetMetricValueOrZero(t, UserSignupsApprovedWithMethodMetric, "method", approvalMethod)
	}
}

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (m *MetricsAssertionHelper) WaitForMetricDelta(t *testing.T, family string, delta float64, labels ...string) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := m.baselineKey(t, family, labels...)
	adjustedValue := m.baselineValues[key] + delta
	m.await.WaitUntiltMetricHasValue(t, family, adjustedValue, labels...)
}

// WaitForMetricBaseline waits for the metric value to reach the baseline value back (to be used during the cleanup)
func (m *MetricsAssertionHelper) WaitForMetricBaseline(t *testing.T, family string, labels ...string) {
	key := m.baselineKey(t, family, labels...)
	m.await.WaitUntiltMetricHasValue(t, family, m.baselineValues[key], labels...)
}

// generates a key to retain the baseline metric value, by joining the metric name and its labels.
// Note: there are probably more sophisticated ways to combine the name and the labels, but for now
// this simple concatenation should be enough to make the keys unique
func (m *MetricsAssertionHelper) baselineKey(t *testing.T, name string, labelAndValues ...string) string {
	if len(labelAndValues)%2 != 0 {
		t.Fatal("`labelAndValues` must be pairs of labels and values")
	}
	return strings.Join(append([]string{name}, labelAndValues...), ",")
}
