package wait

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HostAwaitility the Awaitility for the Host cluster
type HostAwaitility struct {
	*Awaitility
	RegistrationServiceNs  string
	RegistrationServiceURL string
}

// NewHostAwaitility initializes a HostAwaitility
func NewHostAwaitility(t *testing.T, cl client.Client, ns string, registrationServiceNs string) *HostAwaitility {
	return &HostAwaitility{
		Awaitility: &Awaitility{
			T:             t,
			Client:        cl,
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
	}
}

// WaitForMasterUserRecord waits until there is a MasterUserRecord available with the given name and the optional conditions
func (a *HostAwaitility) WaitForMasterUserRecord(name string, criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	var mur *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of MasterUserRecord '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, obj) {
				return false, nil
			}
		}
		a.T.Logf("found MasterUserRecord '%s': %+v with ClusterResources: %v", name, obj, obj.Spec.UserAccounts[0].Spec.NSTemplateSet.ClusterResources)
		mur = obj
		return true, nil
	})
	return mur, err
}

func (a *HostAwaitility) GetMasterUserRecord(criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, mur := range murList.Items {
		for _, match := range criteria {
			if match(a, &mur) {
				a.T.Logf("found MasterUserRecord: %+v", mur)
				return &mur, nil
			}
			a.T.Logf("found MasterUserRecord doesn't match the given criteria: %+v", mur)
		}
	}
	return nil, nil
}

// UpdateMasterUserRecordSpec tries to update the Spec of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordSpec(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(false, murName, modifyMur)
}

// UpdateMasterUserRecordStatus tries to update the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordStatus(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(true, murName, modifyMur)
}

// UpdateMasterUserRecord tries to update the Spec or the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecord(status bool, murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
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
				a.T.Logf("error updating MasterUserRecord.Status '%s': %s. Will retry again...", murName, err.Error())
				return false, nil
			}
		} else if err := a.Client.Update(context.TODO(), freshMur); err != nil {
			a.T.Logf("error updating MasterUserRecord.Spec '%s': %s. Will retry again...", murName, err.Error())
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
func (a *HostAwaitility) UpdateUserSignupSpec(userSignupName string, modifyUserSignup func(us *toolchainv1alpha1.UserSignup)) (*toolchainv1alpha1.UserSignup, error) {
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshUserSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: userSignupName}, freshUserSignup); err != nil {
			return true, err
		}

		modifyUserSignup(freshUserSignup)
		if err := a.Client.Update(context.TODO(), freshUserSignup); err != nil {
			a.T.Logf("error updating UserSignup '%s': %s. Will retry again...", userSignupName, err.Error())
			return false, nil
		}
		userSignup = freshUserSignup
		return true, nil
	})
	return userSignup, err
}

// MasterUserRecordWaitCriterion checks if a MasterUserRecord meets the given condition
type MasterUserRecordWaitCriterion func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool

// UntilMasterUserRecordHasProvisionedTime checks if MasterUserRecord status has the given provisioned time
func UntilMasterUserRecordHasProvisionedTime(expectedTime *v1.Time) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if expectedTime.Time == mur.Status.ProvisionedTime.Time {
			a.T.Logf("MasterUserRecord '%s' status has the expected provisioned time", mur.Name)
			return true
		}
		a.T.Logf("waiting for status of MasterUserRecord '%s' to have the expected provisioned time. Actual: '%s'; Expected: '%s'",
			mur.Name, mur.Status.ProvisionedTime.String(), expectedTime.String())
		return false
	}
}

// UntilMasterUserRecordHasCondition checks if MasterUserRecord status has the given conditions (among others)
func UntilMasterUserRecordHasCondition(condition toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if test.ContainsCondition(mur.Status.Conditions, condition) {
			a.T.Logf("status conditions match in MasterUserRecord '%s`", mur.Name)
			return true
		}
		a.T.Logf("waiting for status condition of MasterUserRecord '%s'. Actual: '%+v'; Expected: '%+v'", mur.Name, mur.Status.Conditions, condition)
		return false
	}
}

// UntilMasterUserRecordHasConditions checks if MasterUserRecord status has the given set of conditions
func UntilMasterUserRecordHasConditions(conditions ...toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if test.ConditionsMatch(mur.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in MasterUserRecord '%s`", mur.Name)
			return true
		}
		a.T.Logf("waiting for status condition of MasterUserRecord '%s'. Actual: '%+v'; Expected: '%+v'", mur.Name, mur.Status.Conditions, conditions)
		return false
	}
}

// UntilMasterUserRecordHasNotSyncIndex checks if MasterUserRecord has a
// sync index *different* from the given value for the given target cluster
func UntilMasterUserRecordHasNotSyncIndex(syncIndex string) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		// lookup user account with target cluster
		ua := mur.Spec.UserAccounts[0]
		a.T.Logf("expecting sync indexes '%s' != '%s'", ua.SyncIndex, syncIndex)
		return ua.SyncIndex != syncIndex
	}
}

func WithMurName(name string) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		return mur.Name == name
	}
}

// UntilMasterUserRecordHasUserAccountStatuses checks if MasterUserRecord status has the given set of status embedded UserAccounts
func UntilMasterUserRecordHasUserAccountStatuses(expUaStatuses ...toolchainv1alpha1.UserAccountStatusEmbedded) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if len(mur.Status.UserAccounts) != len(expUaStatuses) {
			a.T.Logf("waiting for correct number of UserAccount statuses in MasterUserRecord '%s`", mur.Name)
			return false
		}
		for _, expUaStatus := range expUaStatuses {
			expUaStatus.SyncIndex = getUaSpecSyncIndex(mur, expUaStatus.Cluster.Name)
			if !containsUserAccountStatus(mur.Status.UserAccounts, expUaStatus) {
				a.T.Logf("waiting for UserAccount status to be present in MasterUserRecord '%s'. All actual statuses: '%v'; Expected status (to be present among all statuses): '%v'", mur.Name, mur.Status.UserAccounts, expUaStatus)
				return false
			}
		}
		a.T.Logf("all UserAccount statuses are present in MasterUserRecord '%s`", mur.Name)
		return true
	}
}

// UserSignupWaitCriterion a function to check that a user account has the expected condition
type UserSignupWaitCriterion func(a *HostAwaitility, ua *toolchainv1alpha1.UserSignup) bool

// UntilUserSignupHasConditions returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has exactly all the given status conditions
func UntilUserSignupHasConditions(conditions ...toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return func(a *HostAwaitility, ua *toolchainv1alpha1.UserSignup) bool {
		if test.ConditionsMatch(ua.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in UserSignup '%s`", ua.Name)
			return true
		}
		a.T.Logf("waiting for status condition of UserSignup '%s'. Actual: '%+v'; Expected: '%+v'", ua.Name, ua.Status.Conditions, conditions)
		return false
	}
}

// ContainsCondition returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount contains the given status condition
func ContainsCondition(condition toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return func(a *HostAwaitility, ua *toolchainv1alpha1.UserSignup) bool {
		if test.ContainsCondition(ua.Status.Conditions, condition) {
			a.T.Logf("UserSignup '%s' status conditions contains expected condition", ua.Name)
			return true
		}
		a.T.Logf("waiting for status condition of UserSignup '%s' to contain the expected. Actual: '%+v'; Expected: '%+v'", ua.Name, ua.Status.Conditions, condition)
		return false
	}
}

// UntilUserSignupHasStateLabel returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount has toolchain.dev.openshift.com/state equal to the given value
func UntilUserSignupHasStateLabel(expLabelValue string) UserSignupWaitCriterion {
	return func(a *HostAwaitility, ua *toolchainv1alpha1.UserSignup) bool {
		if ua.Labels != nil && ua.Labels[toolchainv1alpha1.UserSignupStateLabelKey] == expLabelValue {
			a.T.Logf("the toolchain.dev.openshift.com/state label value matches '%s`", expLabelValue)
			return true
		}
		a.T.Logf("the toolchain.dev.openshift.com/state label of '%s' UserSignup doesn't match. Actual labels: '%v'; Expected value: '%s'",
			ua.Name, ua.Labels, expLabelValue)
		return false
	}
}

// WaitForTestResourcesCleanup waits for all UserSignup and MasterUserRecord deletions to complete
func (a *HostAwaitility) WaitForTestResourcesCleanup(initialDelay time.Duration) error {
	time.Sleep(initialDelay)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		usList := &toolchainv1alpha1.UserSignupList{}
		if err := a.Client.List(context.TODO(), usList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, us := range usList.Items {
			if us.DeletionTimestamp != nil {
				a.T.Logf("UserSignup is being deleted: %s", us.Name)
				return false, nil
			}
			a.T.Logf("UserSignup is not being deleted: %+v", us)
		}

		murList := &toolchainv1alpha1.MasterUserRecordList{}
		if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, mur := range murList.Items {
			if mur.DeletionTimestamp != nil {
				a.T.Logf("MasterUserRecord is being deleted: %s", mur.Name)
				return false, nil
			}
			a.T.Logf("MasterUserRecord is not being deleted: %+v", mur)
		}
		return true, nil
	})
	return err
}

// WaitForUserSignup waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignup(name string, criteria ...UserSignupWaitCriterion) (*toolchainv1alpha1.UserSignup, error) {
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of UserSignup '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, obj) {
				return false, nil
			}
		}
		a.T.Logf("found UserSignup '%s'", name)
		userSignup = obj
		return true, nil
	})
	return userSignup, err
}

// WaitForBannedUser waits until there is a BannedUser available with the given email
func (a *HostAwaitility) WaitForBannedUser(email string) (bannedUser *toolchainv1alpha1.BannedUser, err error) {
	labels := map[string]string{toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email)}
	opts := client.MatchingLabels(labels)

	err = wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		bannedUserList := &toolchainv1alpha1.BannedUserList{}

		if err = a.Client.List(context.TODO(), bannedUserList, opts); err != nil {
			if len(bannedUserList.Items) == 0 {
				a.T.Logf("waiting for availability of BannedUser with email '%s'", email)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found BannedUser with email '%s'", email)
		bannedUser = &bannedUserList.Items[0]
		return true, nil
	})

	return
}

// DeleteToolchainStatus deletes the ToolchainStatus resource with the given name and in the host operator namespace
func (a *HostAwaitility) DeleteToolchainStatus(name string) error {
	toolchainstatus := &toolchainv1alpha1.ToolchainStatus{}
	if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, toolchainstatus); err != nil {
		if errors.IsNotFound(err) {
			a.T.Logf("ToolchainStatus is already deleted '%s'", name)
			return nil
		}
		return err
	}
	return a.Client.Delete(context.TODO(), toolchainstatus)
}

// WaitUntilBannedUserDeleted waits until the BannedUser with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilBannedUserDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.BannedUser{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("BannedUser is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until BannedUser is deleted '%s'", name)
		return false, nil
	})
}

// WaitUntilUserSignupDeleted waits until the UserSignup with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilUserSignupDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		userSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, userSignup); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("UserSignup is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until UserSignup is deleted '%s'", name)
		return false, nil
	})
}

// WaitUntilMasterUserRecordDeleted waits until the MUR with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilMasterUserRecordDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("MasterUserRecord is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until MasterUserRecord is deleted '%s'", name)
		return false, nil
	})
}

// CheckMasterUserRecordIsDeleted checks that the MUR with the given name is not present and won't be created in the next 2 seconds
func (a *HostAwaitility) CheckMasterUserRecordIsDeleted(name string) {
	err := wait.Poll(a.RetryInterval, 2*time.Second, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("MasterUserRecord is checked as not present '%s'", name)
				return false, nil
			}
			return false, err
		}
		return false, fmt.Errorf("the MasterUserRecord '%s' should not be present, but it is", name)
	})
	require.Equal(a.T, wait.ErrWaitTimeout, err)
}

func getUaSpecSyncIndex(mur *toolchainv1alpha1.MasterUserRecord, cluster string) string {
	for _, ua := range mur.Spec.UserAccounts {
		if ua.TargetCluster == cluster {
			return ua.SyncIndex
		}
	}
	return ""
}

func containsUserAccountStatus(uaStatuses []toolchainv1alpha1.UserAccountStatusEmbedded, uaStatus toolchainv1alpha1.UserAccountStatusEmbedded) bool {
	for _, status := range uaStatuses {
		if reflect.DeepEqual(uaStatus.Cluster, status.Cluster) &&
			uaStatus.SyncIndex == status.SyncIndex &&
			test.ConditionsMatch(uaStatus.Conditions, status.Conditions...) {
			return true
		}
	}
	return false
}

// WaitForNSTemplateTier waits until an NSTemplateTier with the given name exists and matches the given conditions
func (a *HostAwaitility) WaitForNSTemplateTier(name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	var tier *toolchainv1alpha1.NSTemplateTier
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		tier = &toolchainv1alpha1.NSTemplateTier{}
		a.T.Logf("waiting until NSTemplateTier '%s' is created or updated in namespace '%s'...", name, a.Namespace)
		obj := &toolchainv1alpha1.NSTemplateTier{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			a.T.Logf("NSTemplateTier '%s' could not be fetched", name)
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			a.T.Logf("NSTemplateTier '%s' not found in '%s'", name, a.Namespace)
			// keep waiting
			return false, nil
		}
		for _, match := range criteria {
			// if at least one criteria does not match, keep waiting
			if !match(obj) {
				// keep waiting
				a.T.Logf("NSTemplateTier '%s' in namespace '%s' is not matching the expected criteria", name, a.Namespace)
				return false, nil
			}
		}
		// stop waiting
		tier = obj
		return true, nil
	})
	// now, check that the `templateRef` field is set for each namespace and clusterResources (if applicable)
	// and that there's a TierTemplate resource with the same name
	for i, ns := range tier.Spec.Namespaces {
		if ns.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' in namespace #%d in NSTemplateTier '%s'", i, tier.Name)
		}
		if _, err := a.WaitForTierTemplate(ns.TemplateRef); err != nil {
			return nil, err
		}
	}
	if tier.Spec.ClusterResources != nil {
		if tier.Spec.ClusterResources.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' for the cluster resources in NSTemplateTier '%s'", tier.Name)
		}
		if _, err := a.WaitForTierTemplate(tier.Spec.ClusterResources.TemplateRef); err != nil {
			return nil, err
		}
	}
	return tier, err
}

// WaitForTierTemplate waits until a TierTemplate with the given name exists
// Returns an error if the resource did not exist (or something wrong happened)
func (a *HostAwaitility) WaitForTierTemplate(name string) (*toolchainv1alpha1.TierTemplate, error) { // nolint: unparam
	tierTemplate := &toolchainv1alpha1.TierTemplate{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		a.T.Logf("waiting until TierTemplate '%s' exists in namespace '%s'...", name, a.Namespace)
		obj := &toolchainv1alpha1.TierTemplate{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			a.T.Logf("TierTemplate '%s' could not be fetched", name)
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			a.T.Logf("Waiting for TierTemplate '%s' '%s'", name, a.Namespace)
			// keep waiting
			return false, nil
		}
		tierTemplate = obj
		return true, nil
	})
	return tierTemplate, err
}

// NSTemplateTierWaitCriterion the criterion that must be met so the wait is over
type NSTemplateTierWaitCriterion func(*toolchainv1alpha1.NSTemplateTier) bool

// NSTemplateTierSpecMatcher a matcher for the
type NSTemplateTierSpecMatcher func(s toolchainv1alpha1.NSTemplateTierSpec) bool

// UntilNSTemplateTierSpec verify that the NSTemplateTier spec has the specified condition
func UntilNSTemplateTierSpec(match NSTemplateTierSpecMatcher) NSTemplateTierWaitCriterion {
	return func(tier *toolchainv1alpha1.NSTemplateTier) bool {
		return match(tier.Spec)
	}
}

// UntilNSTemplateTierStatusUpdates verify that the NSTemplateTier status.Updates has the specified number of entries
func UntilNSTemplateTierStatusUpdates(count int) NSTemplateTierWaitCriterion {
	return func(tier *toolchainv1alpha1.NSTemplateTier) bool {
		fmt.Printf("tier '%s' status.updates count: actual='%d' vs expected='%d'\n", tier.Name, len(tier.Status.Updates), count)
		return len(tier.Status.Updates) == count
	}
}

// HasNoTemplateRefWithSuffix checks that ALL namespaces' `TemplateRef` doesn't have the suffix
func HasNoTemplateRefWithSuffix(suffix string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		for _, ns := range s.Namespaces {
			if strings.HasSuffix(ns.TemplateRef, suffix) {
				return false
			}
		}
		return !strings.HasSuffix(s.ClusterResources.TemplateRef, suffix)
	}
}

// HasClusterResourcesTemplateRef checks that the clusterResources `TemplateRef` match the given value
func HasClusterResourcesTemplateRef(r string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		return s.ClusterResources.TemplateRef == r
	}
}

// WaitForChangeTierRequest waits until there a ChangeTierRequest is available with the given status conditions
func (a *HostAwaitility) WaitForChangeTierRequest(name string, condition toolchainv1alpha1.Condition) (*toolchainv1alpha1.ChangeTierRequest, error) {
	var changeTierRequest *toolchainv1alpha1.ChangeTierRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of ChangeTierRequest '%s'", name)
				return false, nil
			}
			return false, err
		}
		if test.ConditionsMatch(obj.Status.Conditions, condition) {
			a.T.Logf("status conditions match in ChangeTierRequest '%s`", obj.Name)
			changeTierRequest = obj
			return true, nil
		}
		a.T.Logf("waiting for status condition of ChangeTierRequest '%s'. Actual: '%+v'; Expected: '%+v'",
			obj.Name, obj.Status.Conditions, condition)
		return false, nil
	})
	return changeTierRequest, err
}

// WaitUntilChangeTierRequestDeleted waits until the ChangeTierRequest with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilChangeTierRequestDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		changeTierRequest := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, changeTierRequest); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("ChangeTierRequest has been deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until ChangeTierRequest is deleted '%s'", name)
		return false, nil
	})
}

// WaitForTemplateUpdateRequests waits until there is exactly `count` number of TemplateUpdateRequests
func (a *HostAwaitility) WaitForTemplateUpdateRequests(namespace string, count int) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		templateUpdateRequests := &toolchainv1alpha1.TemplateUpdateRequestList{}
		if err := a.Client.List(context.TODO(), templateUpdateRequests, client.InNamespace(namespace)); err != nil {
			return false, err
		}
		if len(templateUpdateRequests.Items) == count {
			return true, nil
		}
		a.T.Logf("waiting until %d TemplateUpdateRequest(s) are found (current count: %d)", count, len(templateUpdateRequests.Items))
		return false, nil
	})
}

// NotificationWaitCriterion checks if a Notification meets the given condition
type NotificationWaitCriterion func(a *HostAwaitility, mur toolchainv1alpha1.Notification) bool

// WaitForNotifications waits until there is an expected number of Notifications available for the provided user and with the notification type and which match the conditions (if provided).
func (a *HostAwaitility) WaitForNotifications(username, notificationType string, numberOfNotifications int, criteria ...NotificationWaitCriterion) ([]toolchainv1alpha1.Notification, error) {
	var notifications []toolchainv1alpha1.Notification
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}

		actualNotificationCount := len(notificationList.Items)
		if numberOfNotifications != actualNotificationCount {
			a.T.Logf("expected '%d' notifications, but found '%d' notifications", numberOfNotifications, actualNotificationCount)
			return false, nil
		}

		for _, n := range notificationList.Items {
			for _, match := range criteria {
				if !match(a, n) {
					return false, nil
				}
			}
		}
		notifications = notificationList.Items
		return true, nil
	})
	return notifications, err
}

// WaitUntilNotificationsDeleted waits until the Notification for the given user is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilNotificationsDeleted(username, notificationType string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}

		for _, notification := range notificationList.Items {
			a.T.Logf("waiting until Notification is deleted '%s'", notification.Name)
		}
		if len(notificationList.Items) > 0 {
			return false, nil
		}

		a.T.Logf("Notification has been deleted for user '%s'", username)
		return true, nil
	})
}

// UntilNotificationHasConditions checks if Notification status has the given set of conditions
func UntilNotificationHasConditions(conditions ...toolchainv1alpha1.Condition) NotificationWaitCriterion {
	return func(a *HostAwaitility, notification toolchainv1alpha1.Notification) bool {
		if test.ConditionsMatch(notification.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in Notification '%s`", notification.Name)
			return true
		}
		a.T.Logf("waiting for status condition of Notification '%s'. Actual: '%+v'; Expected: '%+v'", notification.Name, notification.Status.Conditions, conditions)
		return false
	}
}

// ToolchainStatusWaitCriterion a function to check that an ToolchainStatus has the expected condition
type ToolchainStatusWaitCriterion func(*HostAwaitility, *toolchainv1alpha1.ToolchainStatus) bool

// UntilToolchainStatusHasConditions returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has exactly all the given status conditions
func UntilToolchainStatusHasConditions(conditions ...toolchainv1alpha1.Condition) ToolchainStatusWaitCriterion {
	return func(a *HostAwaitility, toolchainStatus *toolchainv1alpha1.ToolchainStatus) bool {
		if test.ConditionsMatch(toolchainStatus.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in ToolchainStatus '%s`", toolchainStatus.Name)
			return true
		}
		a.T.Logf("waiting for status condition of ToolchainStatus '%s'. Actual: '%+v'; Expected: '%+v'", toolchainStatus.Name, toolchainStatus.Status.Conditions, conditions)
		return false
	}
}

// UntilAllMembersHaveUsageSet returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has all members with some non-zero resource usage
func UntilAllMembersHaveUsageSet() ToolchainStatusWaitCriterion {
	return func(a *HostAwaitility, toolchainStatus *toolchainv1alpha1.ToolchainStatus) bool {
		for _, member := range toolchainStatus.Status.Members {
			if !hasMemberStatusUsageSet(a.T, member.ClusterName, member.MemberStatus) {
				return false
			}
		}
		return true
	}
}

func UntilAllMembersHaveAPIEndpoint(apiEndpoint string) ToolchainStatusWaitCriterion {
	return func(a *HostAwaitility, toolchainStatus *toolchainv1alpha1.ToolchainStatus) bool {
		//Since all member operators currently run in the same cluster in the e2e test environment, then using the same memberCluster.Spec.APIEndpoint for all the member clusters should be fine.
		for _, member := range toolchainStatus.Status.Members {
			// check Member field ApiEndpoint is assigned
			if member.ApiEndpoint != apiEndpoint {
				return false
			}
		}
		return true
	}
}

// UntilHasMurCount returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has the given count of MasterUserRecords
func UntilHasMurCount(domain string, expectedCount int) ToolchainStatusWaitCriterion {
	return func(a *HostAwaitility, toolchainStatus *toolchainv1alpha1.ToolchainStatus) bool {
		murs, ok := toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
		if !ok {
			a.T.Logf("MasterUserRecordPerDomain metric not found in ToolchainStatus '%s'.", toolchainStatus.Name)
			return false
		}
		if murs[domain] == expectedCount {
			a.T.Logf("MasterUserRecord count matches in ToolchainStatus '%s`", toolchainStatus.Name)
			return true
		}
		a.T.Logf("MasterUserRecord count doesn't match in ToolchainStatus '%s'. Actual: '%d'; Expected: '%d'",
			toolchainStatus.Name, murs[domain], expectedCount)
		return false
	}
}

// WaitForToolchainStatus waits until the ToolchainStatus is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainStatus(criteria ...ToolchainStatusWaitCriterion) (*toolchainv1alpha1.ToolchainStatus, error) {
	// there should only be one toolchain status with the name toolchain-status
	name := "toolchain-status"
	toolchainStatus := &toolchainv1alpha1.ToolchainStatus{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		toolchainStatus = &toolchainv1alpha1.ToolchainStatus{}
		// retrieve the toolchainstatus from the host namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			toolchainStatus)
		if err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for availability of toolchainstatus '%s' in namespace '%s'...\n", name, a.Namespace)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, toolchainStatus) {
				return false, nil
			}
		}
		a.T.Logf("found toolchainstatus '%s': %+v", toolchainStatus.Name, toolchainStatus)
		return true, nil
	})
	return toolchainStatus, err
}

// GetToolchainConfig returns ToolchainConfig instance, nil if not found
func (a *HostAwaitility) GetToolchainConfig() *toolchainv1alpha1.ToolchainConfig {
	config := &toolchainv1alpha1.ToolchainConfig{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, "config"), config); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		require.NoError(a.T, err)
	}
	return config
}

// ToolchainConfigWaitCriterion a function to check that an ToolchainConfig has the expected condition
type ToolchainConfigWaitCriterion func(*HostAwaitility, *toolchainv1alpha1.ToolchainConfig) bool

func UntilToolchainConfigHasSyncedStatus(expectedCondition toolchainv1alpha1.Condition) ToolchainConfigWaitCriterion {
	return func(a *HostAwaitility, toolchainConfig *toolchainv1alpha1.ToolchainConfig) bool {
		if test.ContainsCondition(toolchainConfig.Status.Conditions, expectedCondition) {
			a.T.Logf("status conditions match in ToolchainConfig")
			return true
		}
		a.T.Logf("waiting for status condition of ToolchainConfig. Actual: '%+v'; Expected: '%+v'", toolchainConfig.Status.Conditions, expectedCondition)
		return false
	}
}

// WaitForToolchainConfig waits until the ToolchainConfig is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainConfig(criteria ...ToolchainConfigWaitCriterion) (*toolchainv1alpha1.ToolchainConfig, error) {
	// there should only be one ToolchainConfig with the name "config"
	name := "config"
	toolchainConfig := &toolchainv1alpha1.ToolchainConfig{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		toolchainConfig = &toolchainv1alpha1.ToolchainConfig{}
		// retrieve the ToolchainConfig from the host namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			toolchainConfig)
		if err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for availability of toolchainconfig in namespace '%s'...\n", a.Namespace)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, toolchainConfig) {
				a.T.Logf("Waiting for toolchainconfig to match the expected criteria. Current toolchainconfig: <%+v> \n", toolchainConfig)
				return false, nil
			}
		}
		a.T.Logf("found toolchainconfig '%s': %+v", toolchainConfig.Name, toolchainConfig)
		return true, nil
	})
	return toolchainConfig, err
}

// UpdateToolchainConfig updates the current resource of the ToolchainConfig CR with the given options.
// If there is no existing resource already, then it creates a new one.
// At the end of the test it returns the resource back to the original value/state.
func (a *HostAwaitility) UpdateToolchainConfig(options ...testconfig.ToolchainConfigOption) {
	var originalConfig *toolchainv1alpha1.ToolchainConfig
	// try to get the current ToolchainConfig
	config := a.GetToolchainConfig()
	if config == nil {
		// if it doesn't exist, then create a new one
		config = &toolchainv1alpha1.ToolchainConfig{
			ObjectMeta: v1.ObjectMeta{
				Namespace: a.Namespace,
				Name:      "config",
			},
		}
	} else {
		// if it exists then create a copy to store the original values
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
		require.NoError(a.T, err)

		// and as a cleanup function delete it at the end of the test
		a.T.Cleanup(func() {
			err := a.Client.Delete(context.TODO(), config)
			if err != nil && !errors.IsNotFound(err) {
				require.NoError(a.T, err)
			}
		})
		return
	}

	// if the config did exist before the tests, then update it
	err := a.updateToolchainConfigWithRetry(config)
	require.NoError(a.T, err)

	// and as a cleanup function update it back to the original value
	a.T.Cleanup(func() {
		config := a.GetToolchainConfig()
		// if the current config wasn't found
		if config == nil {
			// then create it back with the original values
			err := a.Client.Create(context.TODO(), originalConfig)
			require.NoError(a.T, err)
		} else {
			// otherwise just update it
			err := a.updateToolchainConfigWithRetry(originalConfig)
			require.NoError(a.T, err)
		}
	})
}

// updateToolchainConfigWithRetry attempts to update the toolchainconfig, helpful because the toolchainconfig controller updates the toolchainconfig
// resource periodically which can cause errors like `Operation cannot be fulfilled on toolchainconfigs.toolchain.dev.openshift.com "config": the object has been modified; please apply your changes to the latest version and try again`
// in some cases. Retrying mitigates the potential for test flakiness due to this behaviour.
func (a *HostAwaitility) updateToolchainConfigWithRetry(updatedConfig *toolchainv1alpha1.ToolchainConfig) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		config := a.GetToolchainConfig()
		config.Spec = updatedConfig.Spec
		if err := a.Client.Update(context.TODO(), config); err != nil {
			a.T.Logf("Retrying ToolchainConfig update due to error: %s", err.Error())
			return false, nil
		}
		return true, nil
	})
	return err
}

// GetHostOperatorPod returns the pod running the host operator controllers
func (a *HostAwaitility) GetHostOperatorPod() (corev1.Pod, error) {
	pods := corev1.PodList{}
	if err := a.Client.List(context.TODO(), &pods, client.InNamespace(a.Namespace), client.MatchingLabels{"name": "host-operator"}); err != nil {
		return corev1.Pod{}, err
	}
	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("unexpected number of pods with label 'name=host-operator' in namespace '%s': %d ", a.Namespace, len(pods.Items))
	}
	return pods.Items[0], nil
}
