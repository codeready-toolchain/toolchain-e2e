package testsupport

import (
	"github.com/codeready-toolchain/toolchain-e2e/wait"
)

type MetricsSupport struct {
	await   *wait.Awaitility
	metrics map[string]float64
}

type metricKey string

const (
	BaseUserSignups                = "sandbox_user_signups_total"
	BaseUserSignupsApproved        = "sandbox_user_signups_approved_total"
	BaseCurrentMURs                = "sandbox_master_user_record_current"
	BaseUserSignupsDeactivated     = "sandbox_user_signups_deactivated_total"
	BaseUserSignupsAutoDeactivated = "sandbox_user_signups_auto_deactivated_total"
)

func InitMetricsAssertion(a *wait.Awaitility) *MetricsSupport {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable

	// Capture baseline values
	m := &MetricsSupport{
		await: a,
	}
	m.captureBaselineValues()
	return m
}

func (m *MetricsSupport) captureBaselineValues() {
	m.metrics[BaseUserSignups] = m.await.GetMetricValue(BaseUserSignups)
	m.metrics[BaseUserSignupsApproved] = m.await.GetMetricValue(BaseUserSignupsApproved)
	m.metrics[BaseCurrentMURs] = m.await.GetMetricValue(BaseCurrentMURs)
	m.metrics[BaseUserSignupsDeactivated] = m.await.GetMetricValue(BaseUserSignupsDeactivated)
	m.metrics[BaseUserSignupsAutoDeactivated] = m.await.GetMetricValue(BaseUserSignupsAutoDeactivated)
}

func (m *MetricsSupport) WaitForMetricValue(k metricKey) {

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
