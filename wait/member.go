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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (a *MemberAwaitility) WaitForNSTmplSet(name string, waitCond ...toolchainv1alpha1.Condition) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		nsTmplSet := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, nsTmplSet); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of NSTemplateSet '%s'", name)
				return false, nil
			}
			return false, err
		}
		if len(waitCond) != 0 && !test.ConditionsMatch(nsTmplSet.Status.Conditions, waitCond...) {
			a.T.Logf("waiting for conditions match for NSTemplateSet '%s'", name)
			return false, nil
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

func (a *MemberAwaitility) GetNamespace(username, typeName string) *v1.Namespace {
	labels := map[string]string{"owner": username, "type": typeName}
	opts := client.MatchingLabels(labels)
	namespaceList := &v1.NamespaceList{}
	err := a.Client.List(context.TODO(), opts, namespaceList)
	require.NoError(a.T, err)
	require.Len(a.T, namespaceList.Items, 1)
	return &namespaceList.Items[0]
}

func (a *MemberAwaitility) WaitForNamespace(username, typeName, revision string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		labels := map[string]string{"owner": username, "type": typeName, "revision": revision}
		opts := client.MatchingLabels(labels)
		namespaceList := &v1.NamespaceList{}
		if err := a.Client.List(context.TODO(), opts, namespaceList); err != nil {
			return false, err
		}

		if len(namespaceList.Items) < 1 {
			a.T.Logf("waiting for availability of namespace of type '%s' with revision '%s' and owned by '%s", typeName, revision, username)
			return false, nil
		}
		require.Len(a.T, namespaceList.Items, 1, "there should be only one Namespace found")
		a.T.Logf("found Namespace type '%s' with revision '%s'", typeName, revision)
		return true, nil
	})
}

func (a *MemberAwaitility) WaitForDeletedNamespace(username, typeName string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		labels := map[string]string{"owner": username, "type": typeName}
		opts := client.MatchingLabels(labels)
		namespaceList := &v1.NamespaceList{}
		if err := a.Client.List(context.TODO(), opts, namespaceList); err != nil {
			return false, err
		}

		if len(namespaceList.Items) < 1 {
			a.T.Logf("deleted Namespace with owner '%s' type '%s'", username, typeName)
			return true, nil
		}
		a.T.Logf("waiting for deletion of Namespace with owner '%s' type '%s'", username, typeName)
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

// WaitForDeletedUserAccount waits until the UserAccount with the given name is not found
func (a *MemberAwaitility) WaitForDeletedUserAccount(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		ua := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, ua); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("UserAccount is checked as deleted '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until UserAccount is deleted '%s'", name)
		return false, nil
	})
}

// WaitForDeletedUser waits until the User with the given name is not found
func (a *MemberAwaitility) WaitForDeletedUser(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		user := &userv1.User{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, user); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("deleted user '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until User is deleted '%s'", name)
		return false, nil
	})
}

// WaitForDeletedIdentity waits until the Identity with the given name is not found
func (a *MemberAwaitility) WaitForDeletedIdentity(name string) error {
	return wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		identity := &userv1.Identity{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, identity); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("deleted identity '%s'", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting until Identity is deleted '%s'", name)
		return false, nil
	})
}
