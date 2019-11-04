package wait

import (
	"context"
	"reflect"

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
		SingleAwaitilityImpl: &SingleAwaitilityImpl{
			T:               a.T,
			Client:          a.Client,
			Ns:              a.HostNs,
			OtherOperatorNs: a.MemberNs,
		}}
}

// WaitForMasterUserRecord waits until there is MasterUserRecord with the given name and the optional conditions is available
func (a *HostAwaitility) WaitForMasterUserRecord(name string, conditions ...MurWaitCondition) (*toolchainv1alpha1.MasterUserRecord, error) {
	mur := &toolchainv1alpha1.MasterUserRecord{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of MasterUserRecord '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, isMatched := range conditions {
			if !isMatched(a, mur) {
				return false, nil
			}
		}
		a.T.Logf("found MasterUserAccount '%s'", name)
		return true, nil
	})
	return mur, err
}

// MurWaitCondition represents a function checking if MasterUserRecord meets the given condition
type MurWaitCondition func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool

// UntilHasStatusCondition checks if MasterUserRecord status has the given set of conditions
func UntilHasStatusCondition(conditions ...toolchainv1alpha1.Condition) MurWaitCondition {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if test.ConditionsMatch(mur.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in MasterUserRecord '%s`", mur.Name)
			return true
		}
		a.T.Logf("waiting for correct status condition of MasterUserRecord '%s`", mur.Name)
		return false
	}
}

// UntilHasUserAccountStatus checks if MasterUserRecord status has the given set of status embedded UserAccounts
func UntilHasUserAccountStatus(expUaStatuses ...toolchainv1alpha1.UserAccountStatusEmbedded) MurWaitCondition {
	return func(a *HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord) bool {
		if len(mur.Status.UserAccounts) != len(expUaStatuses) {
			a.T.Logf("waiting for correct number of UserAccount statuses in MasterUserRecord '%s`", mur.Name)
			return false
		}
		for _, expUaStatus := range expUaStatuses {
			expUaStatus.SyncIndex = getUaSpecSyncIndex(mur, expUaStatus.TargetCluster)
			if !containsUserAccountStatus(mur.Status.UserAccounts, expUaStatus) {
				a.T.Logf("waiting for UserAccount status to be present in MasterUserRecord '%s`", mur.Name)
				return false
			}

		}
		a.T.Logf("all UserAccount statuses are present in MasterUserRecord '%s`", mur.Name)
		return true
	}
}

// WaitForUserSignup waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignup(name string, conditions ...toolchainv1alpha1.Condition) (*toolchainv1alpha1.UserSignup, error) {
	userSignup := &toolchainv1alpha1.UserSignup{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, userSignup); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of UserSignup '%s'", name)
				return false, nil
			}
			return false, err
		}
		if test.ConditionsMatch(userSignup.Status.Conditions, conditions...) {
			a.T.Log("conditions match")
			return true, nil
		}
		a.T.Logf("waiting for [%+v] conditions to match...", conditions)
		return false, nil
	})
	return userSignup, err
}

// WaitUntilMasterUserRecordDeleted waits until MUR with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilMasterUserRecordDeleted(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
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

func getUaSpecSyncIndex(mur *toolchainv1alpha1.MasterUserRecord, targetCluster string) string {
	for _, ua := range mur.Spec.UserAccounts {
		if ua.TargetCluster == targetCluster {
			return ua.SyncIndex
		}
	}
	return ""
}

func containsUserAccountStatus(uaStatuses []toolchainv1alpha1.UserAccountStatusEmbedded, uaStatus toolchainv1alpha1.UserAccountStatusEmbedded) bool {
	for _, status := range uaStatuses {
		if reflect.DeepEqual(uaStatus, status) {
			return true
		}
	}
	return false
}

// WaitForNSTemplateTier waits until an NSTemplateTier with the given name and conditions is present
func (a *HostAwaitility) WaitForNSTemplateTier(name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	tier := &toolchainv1alpha1.NSTemplateTier{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		a.T.Logf("waiting until NSTemplateTier '%s' is created or updated in namespace '%s'...", name, a.Ns)
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, tier)
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
			if !match(tier) {
				// keep waiting
				a.T.Logf("NSTemplateTier '%s' not matching the criteria", name, a.Ns)
				return false, nil
			}
		}
		// stop waiting
		return true, nil
	})
	return tier, err
}

// NSTemplateTierWaitCriterion the criterion that must be met so the wait is over
type NSTemplateTierWaitCriterion func(*toolchainv1alpha1.NSTemplateTier) bool

// NSTemplateTierSpecMatcher a matcher for the
type NSTemplateTierSpecMatcher func(s toolchainv1alpha1.NSTemplateTierSpec) bool

// NSTemplateTierSpecHaving verify that the NSTemplateTier spec has the specified condition
func NSTemplateTierSpecHaving(match NSTemplateTierSpecMatcher) NSTemplateTierWaitCriterion {
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

// NamespaceRevisions checks that ALL namespaces' revision match the given value
func NamespaceRevisions(r string) NSTemplateTierSpecMatcher {
	return func(s toolchainv1alpha1.NSTemplateTierSpec) bool {
		for _, ns := range s.Namespaces {
			if ns.Revision != r {
				return false
			}
		}
		return true
	}
}
