package wait

import (
	"context"
	"reflect"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type MemberAwaitility struct {
	*SingleAwaitilityImpl
}

func NewMemberAwaitility(a *Awaitility) *MemberAwaitility {
	return &MemberAwaitility{
		SingleAwaitilityImpl: &SingleAwaitilityImpl{
			T:               a.T,
			Client:          a.Client,
			Ns:              a.MemberNs,
			OtherOperatorNs: a.HostNs,
		}}
}

// WaitForUserAccount waits until there is a UserAccount available with the given name, expected spec and the set of status conditions
func (a *MemberAwaitility) WaitForUserAccount(name string, expSpec toolchainv1alpha1.UserAccountSpec, conditions ...toolchainv1alpha1.Condition) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		ua := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, ua); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of useraccount '%s'", name)
				return false, nil
			}
			return false, err
		}
		if reflect.DeepEqual(ua.Spec, expSpec) &&
			test.ConditionsMatch(ua.Status.Conditions, conditions...) {
			a.T.Logf("found UserAccount '%s' with expected spec and status condition", name)
			return true, nil
		}
		a.T.Logf("waiting for UserAccount '%s' with expected spec and status condition", name)
		return false, nil
	})
}

func (a *MemberAwaitility) WaitForNSTmplSet(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		nsTmplSet := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, nsTmplSet); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of NSTemplateSet '%s'", name)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found NSTemplateSet '%s'", name)
		return true, nil
	})
}

func (a *MemberAwaitility) WaitForDeletedNSTmplSet(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		nsTmplSet := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, nsTmplSet); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("deleted NSTemplateSet '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting for deletion of NSTemplateSet '%s'", name)
		return false, nil
	})
}

func (a *MemberAwaitility) WaitForNamespace(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		namespace := &v1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, namespace); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of Namespace '%s'", name)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found Namespace '%s'", name)
		return true, nil
	})
}

func (a *MemberAwaitility) WaitForDeletedNamespace(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		namespace := &v1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, namespace); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("deleted Namespace '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting for deletion of Namespace '%s'", name)
		return false, nil
	})
}

// GetUserAccount returns a UserAccount with the given name if is available, otherwise it fails
func (a *MemberAwaitility) GetUserAccount(name string) *toolchainv1alpha1.UserAccount {
	ua := &toolchainv1alpha1.UserAccount{}
	err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, ua)
	require.NoError(a.T, err)
	return ua
}

// WaitForUser waits until there is a User with the given name available
func (a *MemberAwaitility) WaitForUser(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		user := &userv1.User{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, user); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of user '%s'", name)
				return false, nil
			}
			return false, err
		}
		if user.Name != "" && len(user.Identities) > 0 {
			a.T.Logf("found user '%s'", name)
			return true, nil
		}
		return false, nil
	})
}

// WaitForIdentity waits until there is an Identity with the given name available
func (a *MemberAwaitility) WaitForIdentity(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		identity := &userv1.Identity{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, identity); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of identity '%s'", name)
				return false, nil
			}
			return false, err
		}
		if identity.Name != "" && identity.User.Name != "" {
			a.T.Logf("found identity '%s'", name)
			return true, nil
		}
		return false, nil
	})
}
