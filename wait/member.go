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
func (a *MemberAwaitility) WaitForUserAccount(name string, expSpec toolchainv1alpha1.UserAccountSpec, conditions ...toolchainv1alpha1.Condition) (*toolchainv1alpha1.UserAccount, error) {
	ua := &toolchainv1alpha1.UserAccount{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
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
	return ua, err
}

// WaitForNSTmplSet wait until the NSTemplateSet with the given name and conditions exists
func (a *MemberAwaitility) WaitForNSTmplSet(name string, waitCond ...toolchainv1alpha1.Condition) (*toolchainv1alpha1.NSTemplateSet, error) {
	nsTmplSet := &toolchainv1alpha1.NSTemplateSet{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
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
	return nsTmplSet, err
}

// WaitUntilNSTemplateSetDeleted waits until the NSTemplateSet with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNSTemplateSetDeleted(name string) error {
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

// WaitForNamespace waits until a namespace with the given owner (username), type and revision labels exists
func (a *MemberAwaitility) WaitForNamespace(username, typeName, revision string) (*v1.Namespace, error) {
	namespaceList := &v1.NamespaceList{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
		labels := map[string]string{"owner": username, "type": typeName, "revision": revision}
		opts := client.MatchingLabels(labels)
		if err := a.Client.List(context.TODO(), opts, namespaceList); err != nil {
			return false, err
		}

		if len(namespaceList.Items) < 1 {
			a.T.Logf("waiting for availability of Namespace type '%s' with revision '%s'", typeName, revision)
			return false, nil
		}
		require.Len(a.T, namespaceList.Items, 1, "there should be only one Namespace found")
		a.T.Logf("found Namespace type '%s' with revision '%s'", typeName, revision)
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	ns := namespaceList.Items[0]
	return &ns, nil
}

// WaitUntilNamespaceDeleted waits until the namespace with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNamespaceDeleted(username, typeName string) error {
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

// WaitForUser waits until there is a User with the given name available
func (a *MemberAwaitility) WaitForUser(name string) (*userv1.User, error) {
	user := &userv1.User{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
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
	return user, err
}

// WaitForIdentity waits until there is an Identity with the given name available
func (a *MemberAwaitility) WaitForIdentity(name string) (*userv1.Identity, error) {
	identity := &userv1.Identity{}
	err := wait.Poll(RetryInterval, Timeout, func() (done bool, err error) {
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
	return identity, err
}

// WaitUntilUserAccountDeleted waits until the UserAccount with the given name is not found
func (a *MemberAwaitility) WaitUntilUserAccountDeleted(name string) error {
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

// WaitUntilUserDeleted waits until the User with the given name is not found
func (a *MemberAwaitility) WaitUntilUserDeleted(name string) error {
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

// WaitUntilIdentityDeleted waits until the Identity with the given name is not found
func (a *MemberAwaitility) WaitUntilIdentityDeleted(name string) error {
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
