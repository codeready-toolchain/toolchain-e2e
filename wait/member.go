package wait

import (
	"context"
	"reflect"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"

	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
		SingleAwaitilityImpl: NewSingleAwaitility(a.T, a.Client, a.MemberNs, a.HostNs),
	}
}

func (a *MemberAwaitility) WithRetryOptions(options ...interface{}) *MemberAwaitility {
	return &MemberAwaitility{
		SingleAwaitilityImpl: a.SingleAwaitilityImpl.WithRetryOptions(options...),
	}
}

// UserAccountWaitCriterion a function to check that a user account has the expected condition
type UserAccountWaitCriterion func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool

// UntilUserAccountHasSpec returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount has the expected spec
func UntilUserAccountHasSpec(expected toolchainv1alpha1.UserAccountSpec) UserAccountWaitCriterion {
	return func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool {
		a.T.Logf("waiting for useraccount specs. Actual: '%+v'; Expected: '%+v'", ua.Spec, expected)
		return reflect.DeepEqual(ua.Spec, expected)
	}
}

// UntilUserAccountHasConditions returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount has exactly all the given status conditions
func UntilUserAccountHasConditions(conditions ...toolchainv1alpha1.Condition) UserAccountWaitCriterion {
	return func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool {
		if test.ConditionsMatch(ua.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in UserAccount '%s`", ua.Name)
			return true
		}
		a.T.Logf("waiting for status condition of UserSignup '%s'. Acutal: '%+v'; Expected: '%+v'", ua.Name, ua.Status.Conditions, conditions)
		return false
	}
}

// WaitForUserAccount waits until there is a UserAccount available with the given name, expected spec and the set of status conditions
func (a *MemberAwaitility) WaitForUserAccount(name string, criteria ...UserAccountWaitCriterion) (*toolchainv1alpha1.UserAccount, error) {
	var userAccount *toolchainv1alpha1.UserAccount
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of useraccount '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, obj) {
				return false, nil
			}
		}
		a.T.Logf("found UserAccount '%s'", name)
		userAccount = obj
		return true, nil
	})
	return userAccount, err
}

// NSTemplateSetWaitCriterion a function to check that an NSTemplateSet has the expected condition
type NSTemplateSetWaitCriterion func(a *MemberAwaitility, ua *toolchainv1alpha1.NSTemplateSet) bool

// UntilNSTemplateSetHasConditions returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemlateSet has exactly all the given status conditions
func UntilNSTemplateSetHasConditions(conditions ...toolchainv1alpha1.Condition) NSTemplateSetWaitCriterion {
	return func(a *MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet) bool {
		if test.ConditionsMatch(nsTmplSet.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in NSTemplateSet '%s`", nsTmplSet.Name)
			return true
		}
		a.T.Logf("waiting for status condition of NSTemplateSet '%s'. Acutal: '%+v'; Expected: '%+v'", nsTmplSet.Name, nsTmplSet.Status.Conditions, conditions)
		return false
	}
}

// WaitForNSTmplSet wait until the NSTemplateSet with the given name and conditions exists
func (a *MemberAwaitility) WaitForNSTmplSet(name string, criteria ...NSTemplateSetWaitCriterion) (*toolchainv1alpha1.NSTemplateSet, error) {
	var nsTmplSet *toolchainv1alpha1.NSTemplateSet
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Ns}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("waiting for availability of NSTemplateSet '%s'", name)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, obj) {
				return false, nil
			}
		}
		a.T.Logf("found NSTemplateSet '%s'", name)
		nsTmplSet = obj
		return true, nil
	})
	return nsTmplSet, err
}

// WaitUntilNSTemplateSetDeleted waits until the NSTemplateSet with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNSTemplateSetDeleted(name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
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
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		namespaceList = &v1.NamespaceList{}
		labels := map[string]string{
			"toolchain.dev.openshift.com/owner":    username,
			"toolchain.dev.openshift.com/type":     typeName,
			"toolchain.dev.openshift.com/revision": revision,
		}
		opts := client.MatchingLabels(labels)
		if err := a.Client.List(context.TODO(), namespaceList, opts); err != nil {
			return false, err
		}

		if len(namespaceList.Items) < 1 {
			allNSs := &v1.NamespaceList{}
			ls := map[string]string{"provider": "codeready-toolchain"}
			if err := a.Client.List(context.TODO(), allNSs, client.MatchingLabels(ls)); err != nil {
				return false, err
			}
			allNSNames := make(map[string]map[string]string, len(allNSs.Items))
			for _, ns := range allNSs.Items {
				allNSNames[ns.Name] = ns.Labels
			}
			a.T.Logf("waiting for availability of namespace of type '%s' with revision '%s' and owned by '%s'. Currently available codeready-toolchain NSs: '%+v'", typeName, revision, username, allNSNames)
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

// WaitForRoleBinding waits until a RoleBinding with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForRoleBinding(namespace *v1.Namespace, name string) (*rbacv1.RoleBinding, error) {
	roleBinding := &rbacv1.RoleBinding{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &rbacv1.RoleBinding{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allRBs := &rbacv1.RoleBindingList{}
				ls := map[string]string{"provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), allRBs, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of RoleBinding '%s' in namespace '%s'. Currently available codeready-toolchain RoleBindings: '%+v'", name, namespace.Name, allRBs)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found RoleBinding '%s'", name)
		roleBinding = obj
		return true, nil
	})
	return roleBinding, err
}

// WaitForRole waits until a Role with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForRole(namespace *v1.Namespace, name string) (*rbacv1.Role, error) {
	role := &rbacv1.Role{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &rbacv1.Role{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allRoles := &rbacv1.RoleList{}
				ls := map[string]string{"provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), allRoles, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of Role '%s' in namespace '%s'. Currently available codeready-toolchain Roles: '%+v'", name, namespace.Name, allRoles)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found Role '%s'", name)
		role = obj
		return true, nil
	})
	return role, err
}

// WaitUntilNamespaceDeleted waits until the namespace with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNamespaceDeleted(username, typeName string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{
			"toolchain.dev.openshift.com/owner": username,
			"toolchain.dev.openshift.com/type":  typeName,
		}
		opts := client.MatchingLabels(labels)
		namespaceList := &v1.NamespaceList{}
		if err := a.Client.List(context.TODO(), namespaceList, opts); err != nil {
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
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		user = &userv1.User{}
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
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		identity = &userv1.Identity{}
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
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
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
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
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
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
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

// GetConsoleRoute retrieves and returns a Web Console Route
func (a *MemberAwaitility) GetConsoleRoute() (*routev1.Route, error) {
	route := &routev1.Route{}
	namespacedName := types.NamespacedName{Namespace: "openshift-console", Name: "console"}
	err := a.Client.Get(context.TODO(), namespacedName, route)
	if err != nil {
		a.T.Log("didn't find Web Console Route")
	} else {
		a.T.Logf("found %s Web Console Route", route)
	}
	return route, err
}
