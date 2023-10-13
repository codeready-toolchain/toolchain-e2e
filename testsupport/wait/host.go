package wait

import (
	"context"
	"fmt"
	"hash/crc32"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-common/pkg/spacebinding"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testutil "github.com/codeready-toolchain/toolchain-e2e/testsupport/util"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HostAwaitility the Awaitility for the Host cluster
type HostAwaitility struct {
	*Awaitility
	RegistrationServiceNs  string
	RegistrationServiceURL string
	APIProxyURL            string
}

// NewHostAwaitility initializes a HostAwaitility
func NewHostAwaitility(cfg *rest.Config, cl client.Client, ns string, registrationServiceNs string) *HostAwaitility {
	return &HostAwaitility{
		Awaitility: &Awaitility{
			Client:        cl,
			RestConfig:    cfg,
			Namespace:     ns,
			Type:          cluster.Host,
			RetryInterval: DefaultRetryInterval,
			Timeout:       DefaultTimeout,
		},
		RegistrationServiceNs: registrationServiceNs,
	}
}

// WithRetryOptions returns a new HostAwaitility with the given RetryOptions applied
func (a *HostAwaitility) WithRetryOptions(options ...RetryOption) *HostAwaitility {
	return &HostAwaitility{
		Awaitility:             a.Awaitility.WithRetryOptions(options...),
		RegistrationServiceNs:  a.RegistrationServiceNs,
		RegistrationServiceURL: a.RegistrationServiceURL,
		APIProxyURL:            a.APIProxyURL,
	}
}

// WaitForMetricsService verifies that there is a service called `host-operator-metrics-service`
// in the host namespace.
func (a *HostAwaitility) WaitForMetricsService(t *testing.T) {
	_, err := a.WaitForService(t, "host-operator-metrics-service")
	require.NoError(t, err, "failed while waiting for 'host-operator-metrics-service' service")
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

	HostOperatorVersionMetric = "sandbox_host_operator_version"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func (a *HostAwaitility) InitMetrics(t *testing.T, memberClusterNames ...string) {
	// Wait for pending usersignup deletions before capturing baseline values so that test assertions are stable
	err := a.WaitForTestResourcesCleanup(t, 10*time.Second)
	require.NoError(t, err)

	// wait for toolchainstatus metrics to be updated
	_, err = a.WaitForToolchainStatus(t,
		UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		UntilToolchainStatusUpdatedAfter(time.Now()))
	require.NoError(t, err)

	a.WaitForMetricsService(t)
	// Capture baseline values
	a.baselineValues = make(map[string]float64)
	a.baselineValues[UserSignupsMetric] = a.GetMetricValue(t, UserSignupsMetric)
	a.baselineValues[UserSignupsApprovedMetric] = a.GetMetricValue(t, UserSignupsApprovedMetric)
	a.baselineValues[UserSignupsDeactivatedMetric] = a.GetMetricValue(t, UserSignupsDeactivatedMetric)
	a.baselineValues[UserSignupsAutoDeactivatedMetric] = a.GetMetricValue(t, UserSignupsAutoDeactivatedMetric)
	a.baselineValues[UserSignupsBannedMetric] = a.GetMetricValue(t, UserSignupsBannedMetric)
	a.baselineValues[UserSignupVerificationRequiredMetric] = a.GetMetricValue(t, UserSignupVerificationRequiredMetric)
	a.baselineValues[HostOperatorVersionMetric] = a.GetMetricValue(t, HostOperatorVersionMetric)
	for _, name := range memberClusterNames { // sum of gauge value of all member clusters
		spacesKey := a.baselineKey(t, SpacesMetric, "cluster_name", name)
		a.baselineValues[spacesKey] += a.GetMetricValue(t, SpacesMetric, "cluster_name", name)
	}
	// capture `sandbox_users_per_activations_and_domain` with "activations" from `1` to `10` and `internal`/`external` domains
	for i := 1; i <= 10; i++ {
		for _, domain := range []string{"internal", "external"} {
			key := a.baselineKey(t, UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
			a.baselineValues[key] = a.GetMetricValueOrZero(t, UsersPerActivationsAndDomainMetric, "activations", strconv.Itoa(i), "domain", domain)
		}
	}
	for _, domain := range []string{"internal", "external"} {
		key := a.baselineKey(t, MasterUserRecordsPerDomainMetric, "domain", domain)
		a.baselineValues[key] = a.GetMetricValueOrZero(t, MasterUserRecordsPerDomainMetric, "domain", domain)
	}
	for _, approvalMethod := range []string{"automatic", "manual"} {
		key := a.baselineKey(t, UserSignupsApprovedWithMethodMetric, "method", approvalMethod)
		a.baselineValues[key] = a.GetMetricValueOrZero(t, UserSignupsApprovedWithMethodMetric, "method", approvalMethod)
	}

	t.Logf("captured baselines:\n%s", spew.Sdump(a.baselineValues))
}

// WaitForMasterUserRecord waits until there is a MasterUserRecord available with the given name and the optional conditions
func (a *HostAwaitility) WaitForMasterUserRecord(t *testing.T, name string, criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	t.Logf("waiting for MasterUserRecord '%s' in namespace '%s' to match criteria", name, a.Namespace)
	var mur *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		mur = obj
		return matchMasterUserRecordWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printMasterUserRecordWaitCriterionDiffs(t, mur, criteria...)
	}
	return mur, err
}

func (a *HostAwaitility) GetMasterUserRecord(name string) (*toolchainv1alpha1.MasterUserRecord, error) {
	mur := &toolchainv1alpha1.MasterUserRecord{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, name), mur); err != nil {
		return nil, err
	}
	return mur, nil
}

// UpdateMasterUserRecordSpec tries to update the Spec of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordSpec(t *testing.T, murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(t, false, murName, modifyMur)
}

// UpdateMasterUserRecordStatus tries to update the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordStatus(t *testing.T, murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(t, true, murName, modifyMur)
}

// UpdateMasterUserRecord tries to update the Spec or the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecord(t *testing.T, status bool, murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	var m *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshMur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: murName}, freshMur); err != nil {
			return true, err
		}

		modifyMur(freshMur)
		if status {
			// Update status
			if err := a.Client.Status().Update(context.TODO(), freshMur); err != nil {
				t.Logf("error updating MasterUserRecord.Status '%s': %s. Will retry again...", murName, err.Error())
				return false, nil
			}
		} else if err := a.Client.Update(context.TODO(), freshMur); err != nil {
			t.Logf("error updating MasterUserRecord.Spec '%s': %s. Will retry again...", murName, err.Error())
			return false, nil
		}
		m = freshMur
		return true, nil
	})
	return m, err
}

// UpdateUserSignup tries to update the Spec of the given UserSignup
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated UserSignup
func (a *HostAwaitility) UpdateUserSignup(t *testing.T, userSignupName string, modifyUserSignup func(us *toolchainv1alpha1.UserSignup)) (*toolchainv1alpha1.UserSignup, error) {
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshUserSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: userSignupName}, freshUserSignup); err != nil {
			return true, err
		}

		modifyUserSignup(freshUserSignup)
		if err := a.Client.Update(context.TODO(), freshUserSignup); err != nil {
			t.Logf("error updating UserSignup '%s': %s. Will retry again...", userSignupName, err.Error())
			return false, nil
		}
		userSignup = freshUserSignup
		return true, nil
	})
	return userSignup, err
}

// UpdateSpace tries to update the Spec of the given Space
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated Space
func (a *HostAwaitility) UpdateSpace(t *testing.T, spaceName string, modifySpace func(s *toolchainv1alpha1.Space)) (*toolchainv1alpha1.Space, error) {
	var s *toolchainv1alpha1.Space
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshSpace := &toolchainv1alpha1.Space{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: spaceName}, freshSpace); err != nil {
			return true, err
		}
		modifySpace(freshSpace)
		if err := a.Client.Update(context.TODO(), freshSpace); err != nil {
			t.Logf("error updating Space '%s': %s. Will retry again...", spaceName, err.Error())
			return false, nil
		}
		s = freshSpace
		return true, nil
	})
	return s, err
}

// UpdateSpaceBinding tries to update the Spec of the given SpaceBinding
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated SpaceBinding
func (a *HostAwaitility) UpdateSpaceBinding(t *testing.T, spaceBindingName string, modifySpaceBinding func(s *toolchainv1alpha1.SpaceBinding)) (*toolchainv1alpha1.SpaceBinding, error) {
	var s *toolchainv1alpha1.SpaceBinding
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshSpaceBinding := &toolchainv1alpha1.SpaceBinding{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: spaceBindingName}, freshSpaceBinding); err != nil {
			return true, err
		}
		modifySpaceBinding(freshSpaceBinding)
		if err := a.Client.Update(context.TODO(), freshSpaceBinding); err != nil {
			t.Logf("error updating SpaceBinding '%s': %s. Will retry again...", spaceBindingName, err.Error())
			return false, nil
		}
		s = freshSpaceBinding
		return true, nil
	})
	return s, err
}

// MasterUserRecordWaitCriterion a struct to compare with an expected MasterUserRecord
type MasterUserRecordWaitCriterion struct {
	Match func(*toolchainv1alpha1.MasterUserRecord) bool
	Diff  func(*toolchainv1alpha1.MasterUserRecord) string
}

func matchMasterUserRecordWaitCriterion(actual *toolchainv1alpha1.MasterUserRecord, criteria ...MasterUserRecordWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printMasterUserRecordWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.MasterUserRecord, criteria ...MasterUserRecordWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find MasterUserRecord\n")
	} else {
		buf.WriteString("failed to find MasterUserRecord with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) && c.Diff != nil {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	a.listAndPrint(t, "UserSignups", a.Namespace, &toolchainv1alpha1.UserSignupList{})
	a.listAndPrint(t, "MasterUserRecords", a.Namespace, &toolchainv1alpha1.MasterUserRecordList{})
	a.listAndPrint(t, "Spaces", a.Namespace, &toolchainv1alpha1.SpaceList{})

	t.Log(buf.String())
}

// UntilMasterUserRecordIsBeingDeleted checks if MasterUserRecord has Deletion Timestamp
func UntilMasterUserRecordIsBeingDeleted() MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.DeletionTimestamp != nil
		},
	}
}

// UntilMasterUserRecordHasCondition checks if MasterUserRecord status has the given conditions (among others)
func UntilMasterUserRecordHasCondition(expected toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// UntilMasterUserRecordHasConditions checks if MasterUserRecord status has the given set of conditions
func UntilMasterUserRecordHasConditions(expected ...toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

func WithMurName(name string) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.Name == name
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected MasterUserRecord named '%s'", name)
		},
	}
}

// UntilMasterUserRecordHasUserAccountStatuses checks if MasterUserRecord status has the given set of status embedded UserAccounts
func UntilMasterUserRecordHasUserAccountStatuses(expected ...toolchainv1alpha1.UserAccountStatusEmbedded) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			if len(actual.Status.UserAccounts) != len(expected) {
				return false
			}
			for _, expUaStatus := range expected {
				if !containsUserAccountStatus(actual.Status.UserAccounts, expUaStatus) {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected UserAccount statuses to match: %s", Diff(expected, actual.Status.UserAccounts))
		},
	}
}

// UntilMasterUserRecordHasAnyUserAccountStatus checks if MasterUserRecord status has any embedded UserAccount status
func UntilMasterUserRecordHasAnyUserAccountStatus() MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return len(actual.Status.UserAccounts) > 0
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return "expected to be at least one embedded UserAccount status present, but is empty"
		},
	}
}

// UntilMasterUserRecordHasUserAccountStatusesInClusters checks if MasterUserRecord status has a set of UserAccounts provisioned in the given set of clusters
func UntilMasterUserRecordHasUserAccountStatusesInClusters(expectedClusters ...string) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			if len(actual.Status.UserAccounts) != len(expectedClusters) {
				return false
			}
			for _, expectedCluster := range expectedClusters {
				found := false
				for _, ua := range actual.Status.UserAccounts {
					if ua.Cluster.Name == expectedCluster {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected that the status has a list of UserAccounts provisioned in clusters '%s', the actual:\n%v",
				expectedClusters, actual.Status.UserAccounts)
		},
	}
}

func UntilMasterUserRecordHasTierName(expected string) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.Spec.TierName == expected
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected spec.TierName to match: %s", Diff(expected, actual.Spec.TierName))
		},
	}
}

func UntilMasterUserRecordHasNoTierHashLabel() MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			for key := range actual.Labels {
				if strings.HasSuffix(key, "-tier-hash") {
					return false
				}
			}
			return true
		},
	}
}

// UserSignupWaitCriterion a struct to compare with an expected UserSignup
type UserSignupWaitCriterion struct {
	Match func(*toolchainv1alpha1.UserSignup) bool
	Diff  func(*toolchainv1alpha1.UserSignup) string
}

func matchUserSignupWaitCriterion(actual *toolchainv1alpha1.UserSignup, criteria ...UserSignupWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printUserSignupWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.UserSignup, criteria ...UserSignupWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find UserSignup\n")
	} else {
		buf.WriteString("failed to find UserSignup with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) && c.Diff != nil {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// include also all UserSignups in the host namespace, to help troubleshooting
	a.listAndPrint(t, "UserSignups", a.Namespace, &toolchainv1alpha1.UserSignupList{})

	t.Log(buf.String())
}

// UntilUserSignupIsBeingDeleted returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has deletion timestamp set
func UntilUserSignupIsBeingDeleted() UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.DeletionTimestamp != nil
		},
		Diff: func(_ *toolchainv1alpha1.UserSignup) string {
			return "expected a non-nil DeletionTimestamp"
		},
	}
}

// UntilUserSignupHasConditions returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has exactly all the given status conditions
func UntilUserSignupHasConditions(expected ...toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilUserSignupContainsConditions returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup contains all the given status conditions
func UntilUserSignupContainsConditions(shouldContain ...toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			for _, cond := range shouldContain {
				if !test.ContainsCondition(actual.Status.Conditions, cond) {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			return fmt.Sprintf("expected conditions to contain:\n%s", Diff(shouldContain, actual.Status.Conditions))
		},
	}
}

// ContainsCondition returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup contains the given status condition
func ContainsCondition(expected toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// UntilUserSignupHasLabel returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has a `key` equal to the given `value`
func UntilUserSignupHasLabel(key, value string) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.Labels != nil && actual.Labels[key] == value
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			if len(actual.Labels) == 0 {
				return fmt.Sprintf("expected to have a label with key '%s' and value '%s'", key, value)
			}
			return fmt.Sprintf("expected value of label '%s' to equal '%s'. Actual: '%s'", key, value, actual.Labels[key])
		},
	}
}

// UntilUserSignupHasStateLabel returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has toolchain.dev.openshift.com/state equal to the given value
func UntilUserSignupHasStateLabel(expected string) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.Labels != nil && actual.Labels[toolchainv1alpha1.UserSignupStateLabelKey] == expected
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			if len(actual.Labels) == 0 {
				return fmt.Sprintf("expected to have a label with key '%s' and value '%s'", toolchainv1alpha1.UserSignupStateLabelKey, expected)
			}
			return fmt.Sprintf("expected value of label '%s' to equal '%s'. Actual: '%s'", toolchainv1alpha1.UserSignupStateLabelKey, expected, actual.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		},
	}
}

// UntilUserSignupHasCompliantUsername returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has a `.Status.CompliantUsername` value
func UntilUserSignupHasCompliantUsername() UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.Status.CompliantUsername != ""
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			return "expected to have a value for '.Status.CompliantUsername'"
		},
	}
}

// WaitForTestResourcesCleanup waits for all UserSignup, MasterUserRecord, Space, SpaceBinding, NSTemplateSet and Namespace deletions to complete
func (a *HostAwaitility) WaitForTestResourcesCleanup(t *testing.T, initialDelay time.Duration) error {
	t.Logf("waiting for resource cleanup")
	time.Sleep(initialDelay)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		usList := &toolchainv1alpha1.UserSignupList{}
		if err := a.Client.List(context.TODO(), usList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, us := range usList.Items {
			if us.DeletionTimestamp != nil {
				return false, nil
			}
		}

		murList := &toolchainv1alpha1.MasterUserRecordList{}
		if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, mur := range murList.Items {
			if mur.DeletionTimestamp != nil {
				return false, nil
			}
		}

		spaceBindingList := &toolchainv1alpha1.SpaceBindingList{}
		if err := a.Client.List(context.TODO(), spaceBindingList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, spaceBinding := range spaceBindingList.Items {
			if spaceBinding.DeletionTimestamp != nil {
				return false, nil
			}
		}

		spaceList := &toolchainv1alpha1.SpaceList{}
		if err := a.Client.List(context.TODO(), spaceList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, space := range spaceList.Items {
			if space.DeletionTimestamp != nil {
				return false, nil
			}
		}

		nsTemplateSetList := &toolchainv1alpha1.NSTemplateSetList{}
		if err := a.Client.List(context.TODO(), nsTemplateSetList); err != nil {
			return false, err
		}
		for _, nsTemplateSet := range nsTemplateSetList.Items {
			if nsTemplateSet.DeletionTimestamp != nil {
				return false, nil
			}
		}

		namespaceList := &corev1.NamespaceList{}
		if err := a.Client.List(context.TODO(), namespaceList); err != nil {
			return false, err
		}
		for _, namespace := range namespaceList.Items {
			if namespace.DeletionTimestamp != nil {
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForUserSignup waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignup(t *testing.T, name string, criteria ...UserSignupWaitCriterion) (*toolchainv1alpha1.UserSignup, error) {
	t.Logf("waiting for UserSignup '%s' in namespace '%s' to match criteria", name, a.Namespace)
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		userSignup = obj
		return matchUserSignupWaitCriterion(userSignup, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printUserSignupWaitCriterionDiffs(t, userSignup, criteria...)
	}
	return userSignup, err
}

// WaitForUserSignup waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignupByUserIDAndUsername(t *testing.T, userID, username string, criteria ...UserSignupWaitCriterion) (*toolchainv1alpha1.UserSignup, error) {
	t.Logf("waiting for UserSignup '%s' or '%s' in namespace '%s' to match criteria", userID, username, a.Namespace)
	encodedUsername := EncodeUserIdentifier(username)
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: userID}, obj); err != nil {
			if errors.IsNotFound(err) {
				if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: encodedUsername}, obj); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
			} else {
				return false, err
			}
		}
		userSignup = obj
		return matchUserSignupWaitCriterion(userSignup, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printUserSignupWaitCriterionDiffs(t, userSignup, criteria...)
	}
	return userSignup, err
}

// WaitAndVerifyThatUserSignupIsNotCreated waits and checks that the UserSignup is not created
func (a *HostAwaitility) WaitAndVerifyThatUserSignupIsNotCreated(t *testing.T, name string) {
	t.Logf("waiting and verifying that UserSignup '%s' in namespace '%s' is not created", name, a.Namespace)
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		userSignup = obj
		return true, nil
	})
	if err == nil {
		require.Fail(t, fmt.Sprintf("UserSignup '%s' should not be created, but it was found: %v", name, userSignup))
	}
}

// WaitForBannedUser waits until there is a BannedUser available with the given email
func (a *HostAwaitility) WaitForBannedUser(t *testing.T, email string) (*toolchainv1alpha1.BannedUser, error) {
	t.Logf("waiting for BannedUser for user '%s' in namespace '%s'", email, a.Namespace)
	var bannedUser *toolchainv1alpha1.BannedUser
	labels := map[string]string{toolchainv1alpha1.BannedUserEmailHashLabelKey: hash.EncodeString(email)}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		bannedUserList := &toolchainv1alpha1.BannedUserList{}
		if err = a.Client.List(context.TODO(), bannedUserList, client.MatchingLabels(labels), client.InNamespace(a.Namespace)); err != nil {
			if len(bannedUserList.Items) == 0 {
				return false, nil
			}
			return false, err
		}
		bannedUser = &bannedUserList.Items[0]
		return true, nil
	})
	// log message if an error occurred
	if err != nil {
		t.Logf("failed to find Banned for email address '%s': %v", email, err)
	}
	return bannedUser, err
}

// DeleteToolchainStatus deletes the ToolchainStatus resource with the given name and in the host operator namespace
func (a *HostAwaitility) DeleteToolchainStatus(t *testing.T, name string) error {
	t.Logf("deleting ToolchainStatus '%s' in namespace '%s'", name, a.Namespace)
	toolchainstatus := &toolchainv1alpha1.ToolchainStatus{}
	if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, toolchainstatus); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return a.Client.Delete(context.TODO(), toolchainstatus)
}

// WaitUntilBannedUserDeleted waits until the BannedUser with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilBannedUserDeleted(t *testing.T, name string) error {
	t.Logf("waiting until BannedUser '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		user := &toolchainv1alpha1.BannedUser{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, user); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilUserSignupDeleted waits until the UserSignup with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilUserSignupDeleted(t *testing.T, name string) error {
	t.Logf("waiting until UserSignup '%s' in namespace '%s is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		userSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, userSignup); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilMasterUserRecordAndSpaceBindingsDeleted waits until the MUR with the given name and its associated SpaceBindings are deleted (ie, not found)
func (a *HostAwaitility) WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t *testing.T, name string) error {
	t.Logf("waiting until MasterUserRecord '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				// once the MUR is deleted, wait for the associated spacebindings to be deleted as well
				if err := a.WaitUntilSpaceBindingsWithLabelDeleted(t, toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey, name); err != nil {
					return false, err
				}
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// CheckMasterUserRecordIsDeleted checks that the MUR with the given name is not present and won't be created in the next 2 seconds
func (a *HostAwaitility) CheckMasterUserRecordIsDeleted(t *testing.T, name string) {
	t.Logf("checking that MasterUserRecord '%s' in namespace '%s' is deleted", name, a.Namespace)
	err := wait.Poll(a.RetryInterval, 2*time.Second, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return false, fmt.Errorf("the MasterUserRecord '%s' should not be present, but it is", name)
	})
	require.Equal(t, wait.ErrWaitTimeout, err)
}

func containsUserAccountStatus(uaStatuses []toolchainv1alpha1.UserAccountStatusEmbedded, uaStatus toolchainv1alpha1.UserAccountStatusEmbedded) bool {
	for _, status := range uaStatuses {
		if reflect.DeepEqual(uaStatus.Cluster, status.Cluster) &&
			test.ConditionsMatch(uaStatus.Conditions, status.Conditions...) {
			return true
		}
	}
	return false
}

// WaitForUserTier waits until an UserTier with the given name exists and matches any given criteria
func (a *HostAwaitility) WaitForUserTier(t *testing.T, name string, criteria ...UserTierWaitCriterion) (*toolchainv1alpha1.UserTier, error) {
	t.Logf("waiting until UserTier '%s' in namespace '%s' matches criteria", name, a.Namespace)
	tier := &toolchainv1alpha1.UserTier{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserTier{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			// keep waiting
			return false, nil
		}
		tier = obj
		return matchUserTierWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printUserTierWaitCriterionDiffs(t, tier, criteria...)
	}
	return tier, err
}

// UserTierWaitCriterion a struct to compare with an expected UserTier
type UserTierWaitCriterion struct {
	Match func(*toolchainv1alpha1.UserTier) bool
	Diff  func(*toolchainv1alpha1.UserTier) string
}

func matchUserTierWaitCriterion(actual *toolchainv1alpha1.UserTier, criteria ...UserTierWaitCriterion) bool {
	for _, c := range criteria {
		// if at least one criteria does not match, keep waiting
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printUserTierWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.UserTier, criteria ...UserTierWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find UserTier\n")
	} else {
		buf.WriteString("failed to find UserTier with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}

	t.Log(buf.String())
}

// UntilUserTierHasDeactivationTimeoutDays verify that the UserTier status.Updates has the specified number of entries
func UntilUserTierHasDeactivationTimeoutDays(expected int) UserTierWaitCriterion {
	return UserTierWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserTier) bool {
			return actual.Spec.DeactivationTimeoutDays == expected
		},
		Diff: func(actual *toolchainv1alpha1.UserTier) string {
			return fmt.Sprintf("expected deactivationTimeoutDay value %d. Actual: %d", expected, actual.Spec.DeactivationTimeoutDays)
		},
	}
}

func (a *HostAwaitility) WaitUntilBaseUserTierIsUpdated(t *testing.T) error {
	_, err := a.WaitForUserTier(t, "deactivate30", UntilUserTierHasDeactivationTimeoutDays(30))
	return err
}

func (a *HostAwaitility) WaitUntilBaseNSTemplateTierIsUpdated(t *testing.T) error {
	_, err := a.WaitForNSTemplateTier(t, "base", UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
	return err
}

// WaitForNSTemplateTier waits until an NSTemplateTier with the given name exists and matches the given conditions
func (a *HostAwaitility) WaitForNSTemplateTier(t *testing.T, name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	t.Logf("waiting until NSTemplateTier '%s' in namespace '%s' matches criteria", name, a.Namespace)
	tier := &toolchainv1alpha1.NSTemplateTier{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.NSTemplateTier{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			// keep waiting
			return false, nil
		}
		tier = obj
		return matchNSTemplateTierWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNSTemplateTierWaitCriterionDiffs(t, tier, criteria...)
	}
	return tier, err
}

// WaitForNSTemplateTierAndCheckTemplates waits until an NSTemplateTier with the given name exists matching the given conditions and then it verifies that all expected templates exist
func (a *HostAwaitility) WaitForNSTemplateTierAndCheckTemplates(t *testing.T, name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	tier, err := a.WaitForNSTemplateTier(t, name, criteria...)
	if err != nil {
		return nil, err
	}

	// now, check that the `templateRef` field is set for each namespace and clusterResources (if applicable)
	// and that there's a TierTemplate resource with the same name
	for i, ns := range tier.Spec.Namespaces {
		if ns.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' in namespace #%d in NSTemplateTier '%s'", i, tier.Name)
		}
		if _, err := a.WaitForTierTemplate(t, ns.TemplateRef); err != nil {
			return nil, err
		}
	}
	if tier.Spec.ClusterResources != nil {
		if tier.Spec.ClusterResources.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' for the cluster resources in NSTemplateTier '%s'", tier.Name)
		}
		if _, err := a.WaitForTierTemplate(t, tier.Spec.ClusterResources.TemplateRef); err != nil {
			return nil, err
		}
	}
	return tier, err
}

// WaitForTierTemplate waits until a TierTemplate with the given name exists
// Returns an error if the resource did not exist (or something wrong happened)
func (a *HostAwaitility) WaitForTierTemplate(t *testing.T, name string) (*toolchainv1alpha1.TierTemplate, error) { // nolint:unparam
	tierTemplate := &toolchainv1alpha1.TierTemplate{}
	t.Logf("waiting until TierTemplate '%s' exists in namespace '%s'...", name, a.Namespace)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.TierTemplate{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		tierTemplate = obj
		return true, nil
	})
	// log message if an error occurred
	if err != nil {
		t.Logf("failed to find TierTemplate '%s': %v", name, err)
	}
	return tierTemplate, err
}

// NSTemplateTierWaitCriterion a struct to compare with an expected NSTemplateTier
type NSTemplateTierWaitCriterion struct {
	Match func(*toolchainv1alpha1.NSTemplateTier) bool
	Diff  func(*toolchainv1alpha1.NSTemplateTier) string
}

func matchNSTemplateTierWaitCriterion(actual *toolchainv1alpha1.NSTemplateTier, criteria ...NSTemplateTierWaitCriterion) bool {
	for _, c := range criteria {
		// if at least one criteria does not match, keep waiting
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printNSTemplateTierWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.NSTemplateTier, criteria ...NSTemplateTierWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find NSTemplateTier\n")
	} else {
		buf.WriteString("failed to find NSTemplateTier with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// include also all NSTemplateTiers in the host namespace, to help troubleshooting
	a.listAndPrint(t, "NSTemplateTiers", a.Namespace, &toolchainv1alpha1.NSTemplateTierList{})

	t.Log(buf.String())
}

// NSTemplateTierSpecMatcher a struct to compare with an expected NSTemplateTierSpec
type NSTemplateTierSpecMatcher struct {
	Match func(toolchainv1alpha1.NSTemplateTierSpec) bool
	Diff  func(toolchainv1alpha1.NSTemplateTierSpec) string
}

// UntilNSTemplateTierSpec verify that the NSTemplateTier spec has the specified condition
func UntilNSTemplateTierSpec(matcher NSTemplateTierSpecMatcher) NSTemplateTierWaitCriterion {
	return NSTemplateTierWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateTier) bool {
			return matcher.Match(actual.Spec)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateTier) string {
			return matcher.Diff(actual.Spec)
		},
	}
}

// UntilNSTemplateTierStatusUpdates verify that the NSTemplateTier status.Updates has the specified number of entries
func UntilNSTemplateTierStatusUpdates(expected int) NSTemplateTierWaitCriterion {
	return NSTemplateTierWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateTier) bool {
			return len(actual.Status.Updates) == expected
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateTier) string {
			return fmt.Sprintf("expected status.updates count %d. Actual: %d", expected, len(actual.Status.Updates))
		},
	}
}

// HasNoTemplateRefWithSuffix checks that ALL namespaces' `TemplateRef` doesn't have the suffix
func HasNoTemplateRefWithSuffix(suffix string) NSTemplateTierSpecMatcher {
	return NSTemplateTierSpecMatcher{
		Match: func(actual toolchainv1alpha1.NSTemplateTierSpec) bool {
			for _, ns := range actual.Namespaces {
				if strings.HasSuffix(ns.TemplateRef, suffix) {
					return false
				}
			}
			if actual.ClusterResources == nil {
				return false
			}
			return !strings.HasSuffix(actual.ClusterResources.TemplateRef, suffix)
		},
		Diff: func(actual toolchainv1alpha1.NSTemplateTierSpec) string {
			a, _ := yaml.Marshal(actual)
			return fmt.Sprintf("expected no TemplateRef with suffix '%s'. Actual: %s", suffix, a)
		},
	}
}

// HasClusterResourcesTemplateRef checks that the clusterResources `TemplateRef` match the given value
func HasClusterResourcesTemplateRef(expected string) NSTemplateTierSpecMatcher {
	return NSTemplateTierSpecMatcher{
		Match: func(actual toolchainv1alpha1.NSTemplateTierSpec) bool {
			return actual.ClusterResources.TemplateRef == expected
		},
		Diff: func(actual toolchainv1alpha1.NSTemplateTierSpec) string {
			return fmt.Sprintf("expected no ClusterResources.TemplateRef to equal '%s'. Actual: '%s'", expected, actual.ClusterResources.TemplateRef)
		},
	}
}

// NotificationWaitCriterion a struct to compare with an expected Notification
type NotificationWaitCriterion struct {
	Match func(toolchainv1alpha1.Notification) bool
	Diff  func(toolchainv1alpha1.Notification) string
}

func matchNotificationWaitCriterion(actual []toolchainv1alpha1.Notification, criteria ...NotificationWaitCriterion) bool {
	for _, n := range actual {
		for _, c := range criteria {
			if !c.Match(n) {
				return false
			}
		}
	}
	return true
}

func (a *HostAwaitility) printNotificationWaitCriterionDiffs(t *testing.T, actual []toolchainv1alpha1.Notification, criteria ...NotificationWaitCriterion) {
	buf := &strings.Builder{}
	if len(actual) == 0 {
		buf.WriteString("no notification found\n")
	} else {
		buf.WriteString("failed to find notifications with matching criteria:\n")
		buf.WriteString("actual:\n")
		for _, obj := range actual {
			y, _ := StringifyObject(&obj) // nolint:gosec
			buf.Write(y)
		}
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, n := range actual {
			for _, c := range criteria {
				if !c.Match(n) {
					buf.WriteString(c.Diff(n))
					buf.WriteString("\n")
				}
			}
		}
	}
	// include also all Notifications in the host namespace, to help troubleshooting
	a.listAndPrint(t, "Notifications", a.Namespace, &toolchainv1alpha1.NotificationList{})

	t.Log(buf.String())
}

// WaitForNotifications waits until there is an expected number of Notifications available for the provided user and with the notification type and which match the conditions (if provided).
func (a *HostAwaitility) WaitForNotifications(t *testing.T, username, notificationType string, numberOfNotifications int, criteria ...NotificationWaitCriterion) ([]toolchainv1alpha1.Notification, error) {
	t.Logf("waiting for notifications to match criteria for user '%s'", username)
	var notifications []toolchainv1alpha1.Notification
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}
		notifications = notificationList.Items
		if numberOfNotifications != len(notificationList.Items) {
			return false, nil
		}
		return matchNotificationWaitCriterion(notificationList.Items, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNotificationWaitCriterionDiffs(t, notifications, criteria...)
	}
	return notifications, err
}

// WaitForNotificationWithName waits until there is an expected Notifications available with the provided name and with the notification type and which match the conditions (if provided).
func (a *HostAwaitility) WaitForNotificationWithName(t *testing.T, notificationName, notificationType string, criteria ...NotificationWaitCriterion) (toolchainv1alpha1.Notification, error) {
	t.Logf("waiting for notification with name '%s'", notificationName)
	var notification toolchainv1alpha1.Notification
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: notificationName, Namespace: a.Namespace}, &notification); err != nil {
			return false, err
		}
		if typeFound, found := notification.GetLabels()[toolchainv1alpha1.NotificationTypeLabelKey]; !found {
			return false, fmt.Errorf("notification found with name does not have type label")
		} else if typeFound != notificationType {
			return false, fmt.Errorf("notification found with name does not have the expected type")
		}

		return matchNotificationWaitCriterion([]toolchainv1alpha1.Notification{notification}, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNotificationWaitCriterionDiffs(t, []toolchainv1alpha1.Notification{notification}, criteria...)
	}
	return notification, err
}

// WaitUntilNotificationsDeleted waits until the Notification for the given user is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilNotificationsDeleted(t *testing.T, username, notificationType string) error {
	t.Logf("waiting until notifications have been deleted for user '%s'", username)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}
		return len(notificationList.Items) == 0, nil
	})
}

// WaitUntilNotificationWithNameDeleted waits until the Notification with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilNotificationWithNameDeleted(t *testing.T, notificationName string) error {
	t.Logf("waiting for notification with name '%s' to get deleted", notificationName)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		notification := &toolchainv1alpha1.Notification{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: notificationName, Namespace: a.Namespace}, notification); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// UntilNotificationHasConditions checks if Notification status has the given set of conditions
func UntilNotificationHasConditions(expected ...toolchainv1alpha1.Condition) NotificationWaitCriterion {
	return NotificationWaitCriterion{
		Match: func(actual toolchainv1alpha1.Notification) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual toolchainv1alpha1.Notification) string {
			return fmt.Sprintf("expected Notification conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// ToolchainStatusWaitCriterion a struct to compare with an expected ToolchainStatus
type ToolchainStatusWaitCriterion struct {
	Match func(*toolchainv1alpha1.ToolchainStatus) bool
	Diff  func(*toolchainv1alpha1.ToolchainStatus) string
}

func matchToolchainStatusWaitCriterion(actual *toolchainv1alpha1.ToolchainStatus, criteria ...ToolchainStatusWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printToolchainStatusWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.ToolchainStatus, criteria ...ToolchainStatusWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Toolchainstatus\n")
	} else {
		buf.WriteString("failed to find ToolchainStatus with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// include also all ToolchainStatuses in the host namespace, to help troubleshooting
	a.listAndPrint(t, "ToolchainStatuses", a.Namespace, &toolchainv1alpha1.ToolchainStatusList{})

	t.Log(buf.String())
}

// UntilToolchainStatusHasConditions returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has exactly all the given status conditions
func UntilToolchainStatusHasConditions(expected ...toolchainv1alpha1.Condition) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			return fmt.Sprintf("expected ToolchainStatus conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilToolchainStatusUpdated returns a `ToolchainStatusWaitCriterion` which checks that the
// ToolchainStatus ready condition was updated after the given time
func UntilToolchainStatusUpdatedAfter(t time.Time) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			cond, found := condition.FindConditionByType(actual.Status.Conditions, toolchainv1alpha1.ConditionReady)
			return found && t.Before(cond.LastUpdatedTime.Time)
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			cond, found := condition.FindConditionByType(actual.Status.Conditions, toolchainv1alpha1.ConditionReady)
			if !found {
				return fmt.Sprintf("expected ToolchainStatus ready conditions to updated after %s, but it was not found: %v", t.String(), actual.Status.Conditions)
			}
			return fmt.Sprintf("expected ToolchainStatus ready conditions to updated after %s, but is: %v", t.String(), cond.LastUpdatedTime)
		},
	}
}

// UntilAllMembersHaveUsageSet returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has all members with some non-zero resource usage
func UntilAllMembersHaveUsageSet() ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			for _, member := range actual.Status.Members {
				if !hasMemberStatusUsageSet(member.MemberStatus) {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			a, _ := yaml.Marshal(actual.Status.Members)
			return fmt.Sprintf("expected all status members to have usage set. Actual: %s", a)
		},
	}
}

func UntilAllMembersHaveAPIEndpoint(apiEndpoint string) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			//Since all member operators currently run in the same cluster in the e2e test environment, then using the same memberCluster.Spec.APIEndpoint for all the member clusters should be fine.
			for _, member := range actual.Status.Members {
				// check Member field ApiEndpoint is assigned
				if member.APIEndpoint != apiEndpoint {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			a, _ := yaml.Marshal(actual.Status.Members)
			return fmt.Sprintf("expected all status members to have API Endpoint '%s'. Actual: %s", apiEndpoint, a)
		},
	}
}

func UntilProxyURLIsPresent(proxyURL string) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			return strings.TrimSuffix(actual.Status.HostRoutes.ProxyURL, "/") == strings.TrimSuffix(proxyURL, "/")
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			return fmt.Sprintf("Proxy endpoint in the ToolchainStatus doesn't match. Expected: '%s'. Actual: %s", proxyURL, actual.Status.HostRoutes.ProxyURL)
		},
	}
}

// UntilHasMurCount returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has the given count of MasterUserRecords
func UntilHasMurCount(domain string, expectedCount int) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			murs, ok := actual.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
			if !ok {
				return false
			}
			return murs[domain] == expectedCount
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			murs, ok := actual.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
			if !ok {
				return "MasterUserRecordPerDomain metric not found"
			}
			return fmt.Sprintf("expected MasterUserRecordPerDomain metric to be %d. Actual: %d", expectedCount, murs[domain])
		},
	}
}

// UntilHasSpaceCount returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has the given count of Spaces
func UntilHasSpaceCount(clusterName string, expectedCount int) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			for _, m := range actual.Status.Members {
				if m.ClusterName == clusterName {
					return m.SpaceCount == expectedCount
				}
			}
			return false
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			actualCount := 0
			for _, m := range actual.Status.Members {
				if m.ClusterName == clusterName {
					actualCount = m.SpaceCount
				}
			}
			return fmt.Sprintf("expected Space count for cluster %s to be %d. Actual: %d", clusterName, expectedCount, actualCount)
		},
	}
}

// WaitForToolchainStatus waits until the ToolchainStatus is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainStatus(t *testing.T, criteria ...ToolchainStatusWaitCriterion) (*toolchainv1alpha1.ToolchainStatus, error) {
	// there should only be one toolchain status with the name toolchain-status
	name := "toolchain-status"
	toolchainStatus := &toolchainv1alpha1.ToolchainStatus{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ToolchainStatus{}
		// retrieve the toolchainstatus from the host namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		toolchainStatus = obj
		return matchToolchainStatusWaitCriterion(toolchainStatus, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printToolchainStatusWaitCriterionDiffs(t, toolchainStatus, criteria...)
	}
	return toolchainStatus, err
}

// GetToolchainConfig returns ToolchainConfig instance, nil if not found
func (a *HostAwaitility) GetToolchainConfig(t *testing.T) *toolchainv1alpha1.ToolchainConfig {
	config := &toolchainv1alpha1.ToolchainConfig{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, "config"), config); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		require.NoError(t, err)
	}
	return config
}

// ToolchainConfigWaitCriterion a struct to compare with an expected ToolchainConfig
type ToolchainConfigWaitCriterion struct {
	Match func(*toolchainv1alpha1.ToolchainConfig) bool
	Diff  func(*toolchainv1alpha1.ToolchainConfig) string
}

func matchToolchainConfigWaitCriterion(actual *toolchainv1alpha1.ToolchainConfig, criteria ...ToolchainConfigWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printToolchainConfigWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.ToolchainConfig, criteria ...ToolchainConfigWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find ToolchainConfig\n")
	} else {
		buf.WriteString("failed to find ToolchainConfig with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// include also all ToolchainConfigs in the host namespace, to help troubleshooting
	a.listAndPrint(t, "ToolchainConfigs", a.Namespace, &toolchainv1alpha1.ToolchainConfigList{})

	t.Log(buf.String())
}

func UntilToolchainConfigHasSyncedStatus(expected toolchainv1alpha1.Condition) ToolchainConfigWaitCriterion {
	return ToolchainConfigWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainConfig) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainConfig) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// WaitForToolchainConfig waits until the ToolchainConfig is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainConfig(t *testing.T, criteria ...ToolchainConfigWaitCriterion) (*toolchainv1alpha1.ToolchainConfig, error) {
	// there should only be one ToolchainConfig with the name "config"
	name := "config"
	var toolchainConfig *toolchainv1alpha1.ToolchainConfig
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ToolchainConfig{}
		// retrieve the ToolchainConfig from the host namespace
		if err := a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		toolchainConfig = obj
		return matchToolchainConfigWaitCriterion(toolchainConfig, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printToolchainConfigWaitCriterionDiffs(t, toolchainConfig, criteria...)
	}
	return toolchainConfig, err
}

// UpdateToolchainConfig updates the current resource of the ToolchainConfig CR with the given options.
// If there is no existing resource already, then it creates a new one.
// At the end of the test it returns the resource back to the original value/state.
func (a *HostAwaitility) UpdateToolchainConfig(t *testing.T, options ...testconfig.ToolchainConfigOption) {
	var originalConfig *toolchainv1alpha1.ToolchainConfig
	// try to get the current ToolchainConfig
	config := a.GetToolchainConfig(t)
	if config == nil {
		// if it doesn't exist, then create a new one
		config = &toolchainv1alpha1.ToolchainConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: a.Namespace,
				Name:      "config",
			},
		}
	} else {
		// if it exists then create a copied to store the original values
		originalConfig = config.DeepCopy()
	}

	// modify using the given options
	for _, option := range options {
		option.Apply(config)
	}

	// if it didn't exist before
	if originalConfig == nil {
		// then create a new one
		err := a.Client.Create(context.TODO(), config)
		require.NoError(t, err)

		// and as a cleanup function delete it at the end of the test
		t.Cleanup(func() {
			err := a.Client.Delete(context.TODO(), config)
			if err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})
		return
	}

	// if the config did exist before the tests, then update it
	err := a.updateToolchainConfigWithRetry(t, config)
	require.NoError(t, err)

	// and as a cleanup function update it back to the original value
	t.Cleanup(func() {
		config := a.GetToolchainConfig(t)
		// if the current config wasn't found
		if config == nil {
			if originalConfig != nil {
				// then create it back with the original values
				err := a.Client.Create(context.TODO(), originalConfig)
				require.NoError(t, err)
			}
		} else {
			// otherwise just update it
			err := a.updateToolchainConfigWithRetry(t, originalConfig)
			require.NoError(t, err)
		}
	})
}

// updateToolchainConfigWithRetry attempts to update the toolchainconfig, helpful because the toolchainconfig controller updates the toolchainconfig
// resource periodically which can cause errors like `Operation cannot be fulfilled on toolchainconfigs.toolchain.dev.openshift.com "config": the object has been modified; please apply your changes to the latest version and try again`
// in some cases. Retrying mitigates the potential for test flakiness due to this behaviour.
func (a *HostAwaitility) updateToolchainConfigWithRetry(t *testing.T, updatedConfig *toolchainv1alpha1.ToolchainConfig) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		config := a.GetToolchainConfig(t)
		config.Spec = updatedConfig.Spec
		if err := a.Client.Update(context.TODO(), config); err != nil {
			t.Logf("Retrying ToolchainConfig update due to error: %s", err.Error())
			return false, nil
		}
		return true, nil
	})
	return err
}

// GetHostOperatorPod returns the pod running the host operator controllers
func (a *HostAwaitility) GetHostOperatorPod() (corev1.Pod, error) {
	pods := corev1.PodList{}
	if err := a.Client.List(context.TODO(), &pods, client.InNamespace(a.Namespace), client.MatchingLabels{"control-plane": "controller-manager"}); err != nil {
		return corev1.Pod{}, err
	}
	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("unexpected number of pods with label 'control-plane=controller-manager' in namespace '%s': %d ", a.Namespace, len(pods.Items))
	}
	return pods.Items[0], nil
}

// CreateAPIProxyConfig creates a config for the proxy API using the given user token
func (a *HostAwaitility) CreateAPIProxyConfig(t *testing.T, usertoken, proxyURL string) *rest.Config {
	apiConfig, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	require.NoError(t, err)

	defaultConfig, err := testutil.BuildKubernetesRESTConfig(*apiConfig)
	require.NoError(t, err)

	return &rest.Config{
		Host:            proxyURL,
		TLSClientConfig: defaultConfig.TLSClientConfig,
		BearerToken:     usertoken,
	}
}

// CreateAPIProxyClient creates a client to the appstudio api proxy using the given user token
func (a *HostAwaitility) CreateAPIProxyClient(t *testing.T, userToken, proxyURL string) (client.Client, error) {
	proxyKubeConfig := a.CreateAPIProxyConfig(t, userToken, proxyURL)

	s := scheme.Scheme
	builder := append(runtime.SchemeBuilder{}, corev1.AddToScheme)
	require.NoError(t, builder.AddToScheme(s))

	// Getting the proxy client can fail from time to time if the proxy's informer cache has not been
	// updated yet and we try to create the client too quickly so retry to reduce flakiness.
	var proxyCl client.Client
	var initProxyClError error
	waitErr := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		proxyCl, initProxyClError = client.New(proxyKubeConfig, client.Options{Scheme: s})
		return initProxyClError == nil, nil
	})
	if waitErr != nil {
		return nil, initProxyClError
	}
	return proxyCl, nil
}

func (a *HostAwaitility) ProxyURLWithWorkspaceContext(workspaceContext string) string {
	return fmt.Sprintf("%s/workspaces/%s", a.APIProxyURL, workspaceContext)
}

func (a *HostAwaitility) PluginProxyURLWithWorkspaceContext(proxyPluginName, workspaceContext string) string {
	return fmt.Sprintf("%s/plugins/%s/workspaces/%s", a.APIProxyURL, proxyPluginName, workspaceContext)
}

type SpaceWaitCriterion struct {
	Match func(*toolchainv1alpha1.Space) bool
	Diff  func(*toolchainv1alpha1.Space) string
}

func matchSpaceWaitCriterion(actual *toolchainv1alpha1.Space, criteria ...SpaceWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

// WaitForSpace waits until the Space with the given name is available with the provided criteria, if any
func (a *HostAwaitility) WaitForSpace(t *testing.T, name string, criteria ...SpaceWaitCriterion) (*toolchainv1alpha1.Space, error) {
	t.Logf("waiting for Space '%s' with matching criteria", name)
	var space *toolchainv1alpha1.Space
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Space{}
		// retrieve the Space from the host namespace
		if err := a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		space = obj
		return matchSpaceWaitCriterion(space, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSpaceWaitCriterionDiffs(t, space, criteria...)
	}
	return space, err
}

func (a *HostAwaitility) WaitForProxyPlugin(t *testing.T, name string) (*toolchainv1alpha1.ProxyPlugin, error) {
	t.Logf("waiting for ProxyPlugin %q", name)
	var proxyPlugin *toolchainv1alpha1.ProxyPlugin
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ProxyPlugin{}
		if err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
		}
		proxyPlugin = obj
		return true, nil

	})
	return proxyPlugin, err
}

func (a *HostAwaitility) printSpaceWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.Space, criteria ...SpaceWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Space\n")
	} else {
		buf.WriteString("failed to find Space with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include Spaces resources in the host namespace, to help troubleshooting
	a.listAndPrint(t, "Spaces", a.Namespace, &toolchainv1alpha1.SpaceList{})
	t.Log(buf.String())
}

// UntilSpaceIsBeingDeleted checks if Space has Deletion Timestamp
func UntilSpaceIsBeingDeleted() SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.DeletionTimestamp != nil
		},
	}
}

// UntilSpaceHasLabelWithValue returns a `SpaceWaitCriterion` which checks that the given
// Space has the expected label with the given value
func UntilSpaceHasLabelWithValue(key, value string) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Labels[key] == value
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected space to contain label %s:%s:\n%s", key, value, spew.Sdump(actual.Labels))
		},
	}
}

// UntilSpaceHasCreationTimestampOlderThan returns a `SpaceWaitCriterion` which checks that the given
// Space has a timestamp that has elapsed the provided difference duration
func UntilSpaceHasCreationTimestampOlderThan(expectedElapsedTime time.Duration) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			t := time.Now().Add(expectedElapsedTime)
			return t.After(actual.CreationTimestamp.Time)
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected space to be created after %s; Actual creation timestamp %s", expectedElapsedTime.String(), actual.CreationTimestamp.String())
		},
	}
}

// UntilSpaceHasCreationTimestampGreaterThan returns a `SpaceWaitCriterion` which checks that the given
// Space was created after a given creationTimestamp
func UntilSpaceHasCreationTimestampGreaterThan(creationTimestamp time.Time) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.CreationTimestamp.Time.After(creationTimestamp)
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected space to be created after %s; Actual creation timestamp %s", creationTimestamp.String(), actual.CreationTimestamp.String())
		},
	}
}

// UntilSpaceHasTier returns a `SpaceWaitCriterion` which checks that the given
// Space has the expected tier name set in its Spec
func UntilSpaceHasTier(expected string) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Spec.TierName == expected
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected tier name to match:\n%s", Diff(expected, actual.Spec.TierName))
		},
	}
}

// UntilSpaceHasTargetClusterRoles returns a `SpaceWaitCriterion` which checks that the given
// Space has the expected target cluster roles set in its Spec
func UntilSpaceHasTargetClusterRoles(expected []string) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return reflect.DeepEqual(actual.Spec.TargetClusterRoles, expected)
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected target cluster roles to match:\n%s", Diff(expected, actual.Spec.TargetClusterRoles))
		},
	}
}

// UntilSpaceHasConditions returns a `SpaceWaitCriterion` which checks that the given
// Space has exactly all the given status conditions
func UntilSpaceHasConditions(expected ...toolchainv1alpha1.Condition) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilSpaceHasStateLabel returns a `SpaceWaitCriterion` which checks that the
// Space has the expected value of the state label
func UntilSpaceHasStateLabel(expected string) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Labels != nil && actual.Labels[toolchainv1alpha1.SpaceStateLabelKey] == expected
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected Space to match the state label value: %s \nactual labels: %s", expected, actual.Labels)
		},
	}
}

// UntilSpaceHasConditionForTime returns a `SpaceWaitCriterion` which checks that the given
// Space has the condition set at least for the given amount of time
func UntilSpaceHasConditionForTime(expected toolchainv1alpha1.Condition, duration time.Duration) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			foundCond, exists := condition.FindConditionByType(actual.Status.Conditions, expected.Type)
			if exists && foundCond.Reason == expected.Reason && foundCond.Status == expected.Status {
				return foundCond.LastTransitionTime.Time.Before(time.Now().Add(-duration))
			}
			return false
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected conditions to match:\n%s\nAnd having the LastTransitionTime %s or older", Diff(expected, actual.Status.Conditions), time.Now().Add(-duration).String())
		},
	}
}

// UntilSpaceHasAnyProvisionedNamespaces returns a `SpaceWaitCriterion` which checks that the given
// Space has any `status.ProvisionedNamespaces` set
func UntilSpaceHasAnyProvisionedNamespaces() SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return len(actual.Status.ProvisionedNamespaces) > 0
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected provisioned namespaces not to be empty. Actual Space provisioned namespaces:\n%v", actual)
		},
	}
}

// UntilSpaceHasAnyTargetClusterSet returns a `SpaceWaitCriterion` which checks that the given
// Space has any `spec.targetCluster` set
func UntilSpaceHasAnyTargetClusterSet() SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Spec.TargetCluster != ""
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected target clusters not to be empty. Actual Space resource:\n%v", actual)
		},
	}
}

// UntilSpaceHasAnyTierNameSet returns a `SpaceWaitCriterion` which checks that the given
// Space has any `spec.tierName` set
func UntilSpaceHasAnyTierNameSet() SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Spec.TierName != ""
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected tier name not to be empty. Actual Space resource:\n%v", actual)
		},
	}
}

// UntilSpaceHasProvisionedNamespaces returns a `SpaceWaitCriterion` which checks that the given
// Space has the expected `provisionedNamespaces` list in its status
func UntilSpaceHasProvisionedNamespaces(expectedProvisionedNamespaces []toolchainv1alpha1.SpaceNamespace) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return reflect.DeepEqual(expectedProvisionedNamespaces, actual.Status.ProvisionedNamespaces)
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected provisioned namespaces status to match:\n%s", Diff(expectedProvisionedNamespaces, actual.Status.ProvisionedNamespaces))
		},
	}
}

// UntilSpaceHasStatusTargetCluster returns a `SpaceWaitCriterion` which checks that the given
// Space has the expected `targetCluster` in its status
func UntilSpaceHasStatusTargetCluster(expected string) SpaceWaitCriterion {
	return SpaceWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Space) bool {
			return actual.Status.TargetCluster == expected
		},
		Diff: func(actual *toolchainv1alpha1.Space) string {
			return fmt.Sprintf("expected status target clusters to match:\n%s", Diff(expected, actual.Status.TargetCluster))
		},
	}
}

// WaitUntilSpaceAndSpaceBindingsDeleted waits until the Space with the given name and its associated SpaceBindings are deleted (ie, not found)
func (a *HostAwaitility) WaitUntilSpaceAndSpaceBindingsDeleted(t *testing.T, name string) error {
	t.Logf("waiting until Space '%s' in namespace '%s' is deleted", name, a.Namespace)
	var s *toolchainv1alpha1.Space
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Space{}
		if err := a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			}, obj); err != nil {
			if errors.IsNotFound(err) {
				// once the space is deleted, wait for the associated spacebindings to be deleted as well
				if err := a.WaitUntilSpaceBindingsWithLabelDeleted(t, toolchainv1alpha1.SpaceBindingSpaceLabelKey, name); err != nil {
					return false, err
				}
				return true, nil
			}
			return false, err
		}
		s = obj
		return false, nil
	})
	if err != nil {
		y, _ := yaml.Marshal(s)
		t.Logf("Space '%s' was not deleted as expected: %s", name, y)
		return err
	}
	return nil
}

// WaitUntilSpaceBindingDeleted waits until the SpaceBinding with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilSpaceBindingDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.SpaceBinding{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilSpaceBindingsWithLabelDeleted waits until there are no SpaceBindings listed using the given labels
func (a *HostAwaitility) WaitUntilSpaceBindingsWithLabelDeleted(t *testing.T, key, value string) error {
	labels := map[string]string{key: value}
	t.Logf("waiting until SpaceBindings with labels '%v' in namespace '%s' are deleted", labels, a.Namespace)
	var spaceBindingList *toolchainv1alpha1.SpaceBindingList
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		// retrieve the SpaceBinding from the host namespace
		spaceBindingList = &toolchainv1alpha1.SpaceBindingList{}
		if err = a.Client.List(context.TODO(), spaceBindingList, client.MatchingLabels(labels), client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		return len(spaceBindingList.Items) == 0, nil
	})
	// print the listed spacebindings
	if err != nil {
		buf := &strings.Builder{}
		buf.WriteString(fmt.Sprintf("spacebindings still found with labels %v:\n", labels))
		for _, sb := range spaceBindingList.Items {
			y, _ := yaml.Marshal(sb)
			buf.Write(y)
			buf.WriteString("\n")
		}
	}
	return err
}

type SpaceBindingWaitCriterion struct {
	Match func(*toolchainv1alpha1.SpaceBinding) bool
	Diff  func(*toolchainv1alpha1.SpaceBinding) string
}

func matchSpaceBindingWaitCriterion(actual *toolchainv1alpha1.SpaceBinding, criteria ...SpaceBindingWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

// WaitForSubSpace waits until the space provisioned by a SpaceRequest is available with the provided criteria, if any
func (a *HostAwaitility) WaitForSubSpace(t *testing.T, spaceRequestName, spaceRequestNamespace, parentSpaceName string, criteria ...SpaceWaitCriterion) (*toolchainv1alpha1.Space, error) {
	var subSpace *toolchainv1alpha1.Space
	labels := map[string]string{
		toolchainv1alpha1.SpaceRequestLabelKey:          spaceRequestName,
		toolchainv1alpha1.SpaceRequestNamespaceLabelKey: spaceRequestNamespace,
		toolchainv1alpha1.ParentSpaceLabelKey:           parentSpaceName,
	}

	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		// retrieve the subSpace from the host namespace
		spaceList := &toolchainv1alpha1.SpaceList{}
		if err = a.Client.List(context.TODO(), spaceList, client.MatchingLabels(labels), client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		if len(spaceList.Items) == 0 {
			return false, nil
		}
		if len(spaceList.Items) > 1 {
			return false, fmt.Errorf("more than 1 subSpaces for SpaceRequest '%s'", spaceRequestName)
		}
		subSpace = &spaceList.Items[0]
		return matchSpaceWaitCriterion(subSpace, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSpaceWaitCriterionDiffs(t, subSpace, criteria...)
	}
	return subSpace, err
}

// WaitForSpaceBinding waits until the SpaceBinding with the given MUR and Space names is available with the provided criteria, if any
func (a *HostAwaitility) WaitForSpaceBinding(t *testing.T, murName, spaceName string, criteria ...SpaceBindingWaitCriterion) (*toolchainv1alpha1.SpaceBinding, error) {
	var spaceBinding *toolchainv1alpha1.SpaceBinding

	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (bool, error) {
		// retrieve the SpaceBinding from the host namespace
		var err error
		if spaceBinding, err = a.GetSpaceBindingByListing(murName, spaceName); err != nil {
			return false, err
		}
		if spaceBinding == nil {
			return false, nil
		}
		return matchSpaceBindingWaitCriterion(spaceBinding, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSpaceBindingWaitCriterionDiffs(t, spaceBinding, criteria...)
	}
	return spaceBinding, err
}

// GetSpaceBindingByListing lists all available SpaceBinding with the given MUR and Space names set as labels. If none is found, then it returns nil, nil
func (a *HostAwaitility) GetSpaceBindingByListing(murName, spaceName string) (*toolchainv1alpha1.SpaceBinding, error) {
	labels := map[string]string{
		toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey: murName,
		toolchainv1alpha1.SpaceBindingSpaceLabelKey:            spaceName,
	}

	spaceBindingList := &toolchainv1alpha1.SpaceBindingList{}
	if err := a.Client.List(context.TODO(), spaceBindingList, client.MatchingLabels(labels), client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	if len(spaceBindingList.Items) == 0 {
		return nil, nil
	}
	if len(spaceBindingList.Items) > 1 {
		return nil, fmt.Errorf("more than 1 binding for MasterUserRecord '%s' to Space '%s'", murName, spaceName)
	}
	return &spaceBindingList.Items[0], nil
}

func (a *HostAwaitility) printSpaceBindingWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.SpaceBinding, criteria ...SpaceBindingWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find SpaceBinding\n")
	} else {
		buf.WriteString("failed to find SpaceBinding with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include SpaceBindings resources in the host namespace, to help troubleshooting
	a.listAndPrint(t, "SpaceBindings", a.Namespace, &toolchainv1alpha1.SpaceBindingList{})
	t.Log(buf.String())
}

func (a *HostAwaitility) ListSpaceBindings(spaceName string) ([]toolchainv1alpha1.SpaceBinding, error) {
	bindings := &toolchainv1alpha1.SpaceBindingList{}
	if err := a.Client.List(context.TODO(), bindings, client.InNamespace(a.Namespace), client.MatchingLabels{
		toolchainv1alpha1.SpaceBindingSpaceLabelKey: spaceName,
	}); err != nil {
		return nil, err
	}
	return bindings.Items, nil
}

// UntilSpaceBindingHasMurName returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding has the expected MUR name set in its Spec
func UntilSpaceBindingHasMurName(expected string) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.Spec.MasterUserRecord == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			return fmt.Sprintf("expected MUR name to match:\n%s", Diff(expected, actual.Spec.MasterUserRecord))
		},
	}
}

// UntilSpaceBindingHasSpaceName returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding has the expected MUR name set in its Spec
func UntilSpaceBindingHasSpaceName(expected string) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.Spec.Space == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			return fmt.Sprintf("expected Space name to match:\n%s", Diff(expected, actual.Spec.Space))
		},
	}
}

// UntilSpaceBindingHasSpaceRole returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding has the expected SpaceRole name set in its Spec
func UntilSpaceBindingHasSpaceRole(expected string) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.Spec.SpaceRole == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			return fmt.Sprintf("expected Space role to match:\n%s", Diff(expected, actual.Spec.SpaceRole))
		},
	}
}

// UntilSpaceBindingHasDifferentUID returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding has different UID (even if it has same name)
func UntilSpaceBindingHasDifferentUID(uid types.UID) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.UID != uid
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			return fmt.Sprintf("expected SpaceBinding to not have UID %s; Actual UID %s", uid, actual.UID)
		},
	}
}

// UntilSpaceBindingHasCreationTimestampGreaterThan returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding was created after a given creationTimestamp
func UntilSpaceBindingHasCreationTimestampGreaterThan(creationTimestamp time.Time) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.CreationTimestamp.Time.After(creationTimestamp)
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			return fmt.Sprintf("expected SpaceBinding to be created after %s; Actual creation timestamp %s", creationTimestamp.String(), actual.CreationTimestamp.String())
		},
	}
}

// UntilSpaceBindingHasLabel returns a `SpaceBindingWaitCriterion` which checks that the given
// SpaceBinding has a `key` equal to the given `value`
func UntilSpaceBindingHasLabel(key, value string) SpaceBindingWaitCriterion {
	return SpaceBindingWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBinding) bool {
			return actual.Labels != nil && actual.Labels[key] == value
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBinding) string {
			if len(actual.Labels) == 0 {
				return fmt.Sprintf("expected to have a label with key '%s' and value '%s'", key, value)
			}
			return fmt.Sprintf("expected value of label '%s' to equal '%s'. Actual: '%s'", key, value, actual.Labels[key])
		},
	}
}

type SocialEventWaitCriterion struct {
	Match func(*toolchainv1alpha1.SocialEvent) bool
	Diff  func(*toolchainv1alpha1.SocialEvent) string
}

func matchSocialEventWaitCriterion(actual *toolchainv1alpha1.SocialEvent, criteria ...SocialEventWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) WaitForSocialEvent(t *testing.T, name string, criteria ...SocialEventWaitCriterion) (*toolchainv1alpha1.SocialEvent, error) {
	t.Logf("waiting for SocialEvent '%s' in namespace '%s' to match criteria", name, a.Namespace)
	var event *toolchainv1alpha1.SocialEvent
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.SocialEvent{}
		// retrieve the Space from the host namespace
		if err := a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		event = obj
		return matchSocialEventWaitCriterion(event, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSocialEventWaitCriterionDiffs(t, event, criteria...)
	}
	return event, err
}

// UntilSocialEventHasActivationCount returns a `SpaceWaitCriterion` which checks that the
// Space has the expected value of the state label
func UntilSocialEventHasActivationCount(expected int) SocialEventWaitCriterion {
	return SocialEventWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SocialEvent) bool {
			return actual.Status.ActivationCount == expected
		},
		Diff: func(actual *toolchainv1alpha1.SocialEvent) string {
			return fmt.Sprintf("expected SocialEvent to have activation count: %d \nactual: %d", expected, actual.Status.ActivationCount)
		},
	}
}

// UntilSocialEventHasConditions returns a `SocialEventWaitCriterion` which checks that the given
// SocialEvent has exactly all the given status conditions
func UntilSocialEventHasConditions(expected ...toolchainv1alpha1.Condition) SocialEventWaitCriterion {
	return SocialEventWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SocialEvent) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.SocialEvent) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

func (a *HostAwaitility) printSocialEventWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.SocialEvent, criteria ...SocialEventWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find SocialEvent\n")
	} else {
		buf.WriteString("failed to find SocialEvent with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include SocialEvents resources in the host namespace, to help troubleshooting
	a.listAndPrint(t, "SocialEvents", a.Namespace, &toolchainv1alpha1.SocialEventList{})
	t.Log(buf.String())
}

const (
	DNS1123NameMaximumLength         = 63
	DNS1123NotAllowedCharacters      = "[^-a-z0-9]"
	DNS1123NotAllowedStartCharacters = "^[^a-z0-9]+"
)

func EncodeUserIdentifier(subject string) string {
	// Convert to lower case
	encoded := strings.ToLower(subject)

	// Remove all invalid characters
	nameNotAllowedChars := regexp.MustCompile(DNS1123NotAllowedCharacters)
	encoded = nameNotAllowedChars.ReplaceAllString(encoded, "")

	// Remove invalid start characters
	nameNotAllowedStartChars := regexp.MustCompile(DNS1123NotAllowedStartCharacters)
	encoded = nameNotAllowedStartChars.ReplaceAllString(encoded, "")

	// Add a checksum prefix if the encoded value is different to the original subject value
	if encoded != subject {
		encoded = fmt.Sprintf("%x-%s", crc32.Checksum([]byte(subject), crc32.IEEETable), encoded)
	}

	// Trim if the length exceeds the maximum
	if len(encoded) > DNS1123NameMaximumLength {
		encoded = encoded[0:DNS1123NameMaximumLength]
	}

	return encoded
}

// CreateSpaceAndSpaceBinding creates a space and spacebindig and waits until both are present.
// We are creating both of them (Space and SpaceBinding) at the same time , with polling logic, so that we mitigate the issue with spacecleanup_controller deleting the Space before we create it's SpaceBinding.
func (a *HostAwaitility) CreateSpaceAndSpaceBinding(t *testing.T, mur *toolchainv1alpha1.MasterUserRecord, space *toolchainv1alpha1.Space, spaceRole string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBinding, error) {
	var spaceBinding *toolchainv1alpha1.SpaceBinding
	var spaceCreated *toolchainv1alpha1.Space
	t.Logf("Creating Space %s and SpaceBinding for %s", space.Name, mur.Name)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		// create the space
		spaceToCreate := space.DeepCopy()
		if err := a.CreateWithCleanup(t, spaceToCreate); err != nil {
			if !errors.IsAlreadyExists(err) {
				return false, err
			}
		}
		// create spacebinding request immediately after ...
		spaceBinding = spacebinding.NewSpaceBinding(mur, spaceToCreate, spaceRole)
		if err := a.CreateWithCleanup(t, spaceBinding); err != nil {
			if !errors.IsAlreadyExists(err) {
				return false, err
			}
		}
		// let's see if space was provisioned as expected
		spaceCreated = &toolchainv1alpha1.Space{}
		err = a.Client.Get(context.TODO(), client.ObjectKeyFromObject(spaceToCreate), spaceCreated)
		if err != nil {
			if errors.IsNotFound(err) {
				t.Logf("The created Space %s is not present", spaceCreated.Name)
				return false, nil
			}
			return false, err
		}
		if util.IsBeingDeleted(spaceCreated) {
			// space is in terminating let's wait until is gone and recreate it ...
			t.Logf("The created Space %s is being deleted", spaceCreated.Name)
			return false, a.WaitUntilSpaceAndSpaceBindingsDeleted(t, spaceCreated.Name)
		}
		// let's see if SpaceBinding was provisioned as expected
		spaceBinding, err = a.GetSpaceBindingByListing(mur.Name, spaceCreated.Name)
		if err != nil {
			return false, err
		}
		if spaceBinding == nil {
			t.Logf("The created SpaceBinding %s is not present", spaceCreated.Name)
			return false, nil
		}
		if util.IsBeingDeleted(spaceBinding) {
			// spacebinding is in terminating let's wait until is gone and recreate it ...
			t.Logf("The created SpaceBinding %s is being deleted", spaceBinding.Name)
			return false, a.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		}
		t.Logf("Space %s and SpaceBinding %s created", spaceCreated.Name, spaceBinding.Name)
		return true, nil
	})
	return spaceCreated, spaceBinding, err
}
