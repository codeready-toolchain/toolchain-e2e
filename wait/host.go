package wait

import (
	"context"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"reflect"
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

// WaitForMasterUserRecord waits until there is MasterUserRecord with the given name available
func (a *HostAwaitility) WaitForMasterUserRecord(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of MasterUserRecord '%s'", name)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found MasterUserAccount '%s'", name)
		return true, nil
	})
}

// GetMasterUserRecord returns MasterUserRecord with the given name if available, otherwise it fails
func (a *HostAwaitility) GetMasterUserRecord(name string) *toolchainv1alpha1.MasterUserRecord {
	mur := &toolchainv1alpha1.MasterUserRecord{}
	err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur)
	require.NoError(a.T, err)
	return mur
}

// WaitForMasterUserRecord waits until there is MasterUserRecord available with the given name and meeting the set of given wait-conditions
func (a *HostAwaitility) WaitForMurConditions(name string, waitCond ...MurWaitCondition) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of MasterUserRecord '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, isMatched := range waitCond {
			if !isMatched(a, mur) {
				return false, nil
			}
		}
		return true, nil
	})
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

// WaitForUserSignupStatusConditions waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignupStatusConditions(name string, conditions ...toolchainv1alpha1.Condition) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		userSignup := &toolchainv1alpha1.UserSignup{}
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
}

// WaitForUserSignup waits until there is a UserSignup with the given name available
func (a *HostAwaitility) WaitForUserSignup(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		userSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, userSignup); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of UserSignup '%s'", name)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found UserSignup '%s'", name)
		return true, nil
	})
}

// WaitForDeletedMasterUserRecord waits until MUR with the given name is not present
func (a *HostAwaitility) WaitForDeletedMasterUserRecord(name string) error {
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
