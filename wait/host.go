package wait

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/stretchr/testify/require"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HostAwaitility the Awaitility for the Host cluster
type HostAwaitility struct {
	*Awaitility
	FrameworkClient        framework.FrameworkClient
	RegistrationServiceNs  string
	RegistrationServiceURL string
}

// NewHostAwaitility initializes a HostAwaitility
func NewHostAwaitility(t *testing.T, fcl framework.FrameworkClient, cl client.Client, ns string, registrationServiceNs string) *HostAwaitility {
	return &HostAwaitility{
		Awaitility: &Awaitility{
			T:             t,
			Client:        cl,
			Namespace:     ns,
			Type:          cluster.Host,
			RetryInterval: DefaultRetryInterval,
			Timeout:       DefaultTimeout,
		},
		FrameworkClient:       fcl,
		RegistrationServiceNs: registrationServiceNs,
	}
}

// WithRetryOptions returns a new HostAwaitility with the given RetryOptions applied
func (a *HostAwaitility) WithRetryOptions(options ...RetryOption) *HostAwaitility {
	return &HostAwaitility{
		Awaitility:             a.Awaitility.WithRetryOptions(options...),
		FrameworkClient:        a.FrameworkClient,
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

// UpdateMasterUserRecord tries to update the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and
func (a *HostAwaitility) UpdateMasterUserRecord(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshMur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: murName}, freshMur); err != nil {
			return true, err
		}

		modifyMur(freshMur)
		if err := a.Client.Update(context.TODO(), freshMur); err != nil {
			a.T.Logf("error updating MasterUserRecord '%s': %s. Will retry again...", murName, err.Error())
			return false, nil
		}
		return true, nil
	})
}

// MasterUserRecordWaitCriterion checks if a MasterUserRecord meets the given condition
type MasterUserRecordWaitCriterion func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool

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
// USerAccount has exactly all the given status conditions
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

// WaitUntilMasterUserRecordDeleted waits until MUR with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilMasterUserRecordDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("MasterUserAccount is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until MasterUserAccount is deleted '%s'", name)
		return false, nil
	})
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

// Not negates the given matcher
func Not(match NSTemplateTierSpecMatcher) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		return !match(s)
	}
}

// Has.NSTemplateRefs checks that ALL namespaces' `TemplateRef` match the given value
func HasNSTemplateRefs(r string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		for _, ns := range s.Namespaces {
			if ns.TemplateRef != r {
				return false
			}
		}
		return true
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
type NotificationWaitCriterion func(a *HostAwaitility, mur *toolchainv1alpha1.Notification) bool

// WaitForNotification waits until there is a Notification available with the given name and the optional conditions
func (a *HostAwaitility) WaitForNotification(name string, criteria ...NotificationWaitCriterion) (*toolchainv1alpha1.Notification, error) {
	var notification *toolchainv1alpha1.Notification
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Notification{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of notification '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, obj) {
				return false, nil
			}
		}
		a.T.Logf("found notification '%s'", name)
		notification = obj
		return true, nil
	})
	return notification, err
}

// WaitUntilNotificationDeleted waits until the Notification with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilNotificationDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		notification := &toolchainv1alpha1.Notification{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, notification); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Notification has been deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until Notification is deleted '%s'", name)
		return false, nil
	})
}

// UntilNotificationHasConditions checks if Notification status has the given set of conditions
func UntilNotificationHasConditions(conditions ...toolchainv1alpha1.Condition) NotificationWaitCriterion {
	return func(a *HostAwaitility, notification *toolchainv1alpha1.Notification) bool {
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

// UntilHasMurCount returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has the given count of MasterUserRecords
func UntilHasMurCount(murCount int) ToolchainStatusWaitCriterion {
	return func(a *HostAwaitility, toolchainStatus *toolchainv1alpha1.ToolchainStatus) bool {
		if toolchainStatus.Status.HostOperator != nil {
			if toolchainStatus.Status.HostOperator.MasterUserRecordCount == murCount {
				a.T.Logf("MasterUserRecord count matches in ToolchainStatus '%s`", toolchainStatus.Name)
				return true
			}
			murList := &toolchainv1alpha1.MasterUserRecordList{}
			err := a.Client.List(context.TODO(), murList, client.InNamespace(toolchainStatus.Namespace))
			require.NoError(a.T, err)
			a.T.Logf("MasterUserRecord count doesn't match in ToolchainStatus '%s'. Actual: '%d'; Expected: '%d'. The actual number of MURs is: '%d'",
				toolchainStatus.Name, toolchainStatus.Status.HostOperator.MasterUserRecordCount, murCount, len(murList.Items))
		} else {
			a.T.Logf("HostOperator status part in ToolchainStatus is nil '%s'", toolchainStatus.Name)
		}
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
