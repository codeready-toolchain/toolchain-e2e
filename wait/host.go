package wait

import (
	"context"
	"reflect"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type HostAwaitility struct {
	*SingleAwaitilityImpl
}

func NewHostAwaitility(a *Awaitility) *HostAwaitility {
	return &HostAwaitility{
		SingleAwaitilityImpl: NewSingleAwaitility(a.T, a.Client, a.HostNs, a.MemberNs),
	}
}

func (a *HostAwaitility) WithRetryOptions(options ...interface{}) *HostAwaitility {
	return &HostAwaitility{
		SingleAwaitilityImpl: a.SingleAwaitilityImpl.WithRetryOptions(options...),
	}
}

// WaitForMasterUserRecord waits until there is MasterUserRecord with the given name and the optional conditions is available
func (a *HostAwaitility) WaitForMasterUserRecord(name string, criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	var mur *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, obj); err != nil {
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
	if err := a.Client.List(context.TODO(), murList); err != nil {
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

func (a *HostAwaitility) GetNotification(criteria ...NotificationWaitCriterion) (*toolchainv1alpha1.Notification, error) {
	notificationList := &toolchainv1alpha1.Notification{}
	if err := a.Client.List(context.TODO(), notificationList); err != nil {
		return nil, err
	}
	for _, notification := range notificationList.Items {
		for _, match := range criteria {
			if match(a, &notification) {
				a.T.Logf("found notification: %+v", notification)
				return &notification, nil
			}
			a.T.Logf("found notification doesn't match the given criteria: %+v", notification)
		}
	}
	return nil, nil
}

// UpdateMasterUserRecord tries to update the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and
func (a *HostAwaitility) UpdateMasterUserRecord(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshMur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: murName}, freshMur); err != nil {
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

// MasterUserRecordWaitCriterion represents a function checking if MasterUserRecord meets the given condition
type MasterUserRecordWaitCriterion func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool

type NotificationWaitCriterion func(a *HostAwaitility, mur *toolchainv1alpha1.Notification) bool

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

func WithMurName(name string) MasterUserRecordWaitCriterion {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		return mur.Name == name
	}
}

func WithTemplate(notificationName, notificationType string) NotificationWaitCriterion {
	return func(a *HostAwaitility, notification *toolchainv1alpha1.Notification) bool {
		return notification.Name == notificationName
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
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, obj); err != nil {
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
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur); err != nil {
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
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur); err != nil {
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

// WaitForNSTemplateTier waits until an NSTemplateTier with the given name and conditions is present
func (a *HostAwaitility) WaitForNSTemplateTier(name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	var tier *toolchainv1alpha1.NSTemplateTier
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		tier = &toolchainv1alpha1.NSTemplateTier{}
		a.T.Logf("waiting until NSTemplateTier '%s' is created or updated in namespace '%s'...", name, a.Ns)
		obj := &toolchainv1alpha1.NSTemplateTier{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			a.T.Logf("NSTemplateTier '%s' could not be fetched", name)
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			a.T.Logf("NSTemplateTier '%s' not found in '%s'", name, a.Ns)
			// keep waiting
			return false, nil
		}
		for _, match := range criteria {
			// if at least one criteria does not match, keep waiting
			if !match(obj) {
				// keep waiting
				a.T.Logf("NSTemplateTier '%s' in namespace '%s' is not matching the expected criteria", name, a.Ns)
				return false, nil
			}
		}
		// stop waiting
		tier = obj
		return true, nil
	})
	return tier, err
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

// Not negates the given matcher
func Not(match NSTemplateTierSpecMatcher) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		return !match(s)
	}
}

// HasNamespaceRevisions checks that ALL namespaces' revision match the given value
func HasNamespaceRevisions(r string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		for _, ns := range s.Namespaces {
			if ns.Revision != r {
				return false
			}
		}
		return true
	}
}

// HasClusterResources checks that the clusterResources revision match the given value
func HasClusterResources(r string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		return s.ClusterResources.Revision == r
	}
}

// WaitForChangeTierRequest waits until there a ChangeTierRequest is available with the given status conditions
func (a *HostAwaitility) WaitForChangeTierRequest(name string, condition toolchainv1alpha1.Condition) (*toolchainv1alpha1.ChangeTierRequest, error) {
	var changeTierRequest *toolchainv1alpha1.ChangeTierRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, obj); err != nil {
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

// WaitUntilChangeTierRequestDeleted waits until ChangeTierRequest with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilChangeTierRequestDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		changeTierRequest := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, changeTierRequest); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("ChangeTierRequest is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until ChangeTierRequest is deleted '%s'", name)
		return false, nil
	})
}
