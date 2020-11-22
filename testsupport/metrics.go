package testsupport

import (
	"time"
)

type metricsSupport struct {
	await          metricsProvider
	baselineValues map[string]float64
}

type metricKey string

type metricsProvider interface {
	GetMetricValue(family string, labels ...string) float64
	WaitForUserSignupsBeingDeleted(initialDelay time.Duration) error
	AssertMetricReachesValue(family string, expectedValue float64, labels ...string)
}

const (
	UserSignupsMetric                = "sandbox_user_signups_total"
	UserSignupsApprovedMetric        = "sandbox_user_signups_approved_total"
	CurrentMURsMetric                = "sandbox_master_user_record_current"
	UserSignupsDeactivatedMetric     = "sandbox_user_signups_deactivated_total"
	UserSignupsAutoDeactivatedMetric = "sandbox_user_signups_auto_deactivated_total"
	UserSignupsBannedMetric          = "sandbox_user_signups_banned_total"
)

func InitMetricsAssertion(a metricsProvider) *metricsSupport {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	a.WaitForUserSignupsBeingDeleted(5 * time.Second)

	// Capture baseline values
	m := &metricsSupport{
		await: a,
	}
	m.captureBaselineValues()
	return m
}

func (m *metricsSupport) captureBaselineValues() {
	m.baselineValues[UserSignupsMetric] = m.await.GetMetricValue(UserSignupsMetric)
	m.baselineValues[UserSignupsApprovedMetric] = m.await.GetMetricValue(UserSignupsApprovedMetric)
	m.baselineValues[CurrentMURsMetric] = m.await.GetMetricValue(CurrentMURsMetric)
	m.baselineValues[UserSignupsDeactivatedMetric] = m.await.GetMetricValue(UserSignupsDeactivatedMetric)
	m.baselineValues[UserSignupsAutoDeactivatedMetric] = m.await.GetMetricValue(UserSignupsAutoDeactivatedMetric)
}

func (m *metricsSupport) WaitForMetricDelta(family metricKey, delta float64) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := string(family)
	adjustedValue := m.baselineValues[key] + delta
	m.await.AssertMetricReachesValue(key, adjustedValue)
}

// func (a *HostAwaitility) GetUserSignup(criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
// 	murList := &toolchainv1alpha1.UserSignupList{}
// 	if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
// 		return nil, err
// 	}
// 	for _, mur := range murList.Items {
// 		for _, match := range criteria {
// 			if match(a, &mur) {
// 				a.T.Logf("found MasterUserRecord: %+v", mur)
// 				return &mur, nil
// 			}
// 			a.T.Logf("found MasterUserRecord doesn't match the given criteria: %+v", mur)
// 		}
// 	}
// 	return nil, nil
// }
