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
	t              *testing.T
}

type metricsProvider interface {
	GetMetricValue(family string, labels ...string) float64
	GetMetricValueOrZero(family string, labels ...string) float64
	WaitForTestResourcesCleanup(initialDelay time.Duration) error
	WaitUntiltMetricHasValue(family string, expectedValue float64, labels ...string)
}

// metric constants
const (
	UserSignupsMetric                = "sandbox_user_signups_total"
	UserSignupsApprovedMetric        = "sandbox_user_signups_approved_total"
	UserSignupsDeactivatedMetric     = "sandbox_user_signups_deactivated_total"
	UserSignupsAutoDeactivatedMetric = "sandbox_user_signups_auto_deactivated_total"
	UserSignupsBannedMetric          = "sandbox_user_signups_banned_total"

	MasterUserRecordsPerDomainMetric = "sandbox_master_user_records"

	UserAccountsMetric = "sandbox_user_accounts_current"

	SpacesMetric = "sandbox_spaces_current"

	UsersPerActivationsAndDomainMetric = "sandbox_users_per_activations_and_domain"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func InitMetricsAssertion(t *testing.T, awaitilities wait.Awaitilities) *MetricsAssertionHelper {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	err := awaitilities.Host().WaitForTestResourcesCleanup(5 * time.Second)
	require.NoError(t, err)

	// Capture baseline values
	m := &MetricsAssertionHelper{
		await:          awaitilities.Host(),
		baselineValues: make(map[string]float64),
		t:              t,
	}
	m.captureBaselineValues(awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Logf("captured baselines:\n%s", spew.Sdump(m.baselineValues))
	return m
}

func (m *MetricsAssertionHelper) captureBaselineValues(memberClusterNames ...string) {
	m.baselineValues[UserSignupsMetric] = m.await.GetMetricValue(UserSignupsMetric)
	m.baselineValues[UserSignupsApprovedMetric] = m.await.GetMetricValue(UserSignupsApprovedMetric)
	m.baselineValues[UserSignupsDeactivatedMetric] = m.await.GetMetricValue(UserSignupsDeactivatedMetric)
	m.baselineValues[UserSignupsAutoDeactivatedMetric] = m.await.GetMetricValue(UserSignupsAutoDeactivatedMetric)
	m.baselineValues[UserSignupsBannedMetric] = m.await.GetMetricValue(UserSignupsBannedMetric)
	for _, name := range memberClusterNames { // sum of gauge value of all member clusters
		userAccountKey := m.baselineKey(UserAccountsMetric, "cluster_name", name)
		m.baselineValues[userAccountKey] += m.await.GetMetricValue(UserAccountsMetric, "cluster_name", name)

		spacesKey := m.baselineKey(SpacesMetric, "cluster_name", name)
		m.baselineValues[spacesKey] += m.await.GetMetricValue(SpacesMetric, "cluster_name", name)
	}
	// capture `sandbox_users_per_activations_and_domain` with "activations" from `1` to `10` and `internal`/`external` domains
	for i := 1; i <= 10; i++ {
		for _, domain := range []string{"internal", "external"} {
			key := m.baselineKey(UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
			m.baselineValues[key] = m.await.GetMetricValueOrZero(UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
		}
	}
	for _, domain := range []string{"internal", "external"} {
		key := m.baselineKey(MasterUserRecordsPerDomainMetric, "domain", domain)
		m.baselineValues[key] = m.await.GetMetricValueOrZero(MasterUserRecordsPerDomainMetric, "domain", domain)
	}
}

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (m *MetricsAssertionHelper) WaitForMetricDelta(family string, delta float64, labels ...string) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := m.baselineKey(string(family), labels...)
	adjustedValue := m.baselineValues[key] + delta
	m.await.WaitUntiltMetricHasValue(string(family), adjustedValue, labels...)
}

// generates a key to retain the baseline metric value, by joining the metric name and its labels.
// Note: there are probably more sophisticated ways to combine the name and the labels, but for now
// this simple concatenation should be enough to make the keys unique
func (m *MetricsAssertionHelper) baselineKey(name string, labelAndValues ...string) string {
	if len(labelAndValues)%2 != 0 {
		m.t.Fatal("`labelAndValues` must be pairs of labels and values")
	}
	return strings.Join(append([]string{name}, labelAndValues...), ",")
}
