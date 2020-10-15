package wait

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"

	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MemberAwaitility struct {
	*Awaitility
}

func NewMemberAwaitility(t *testing.T, cl client.Client, ns string) *MemberAwaitility {
	return &MemberAwaitility{
		Awaitility: &Awaitility{
			Client:        cl,
			T:             t,
			Namespace:     ns,
			Type:          cluster.Member,
			RetryInterval: DefaultRetryInterval,
			Timeout:       DefaultTimeout,
		},
	}
}

func (a *MemberAwaitility) WithRetryOptions(options ...RetryOption) *MemberAwaitility {
	return &MemberAwaitility{
		Awaitility: a.Awaitility.WithRetryOptions(options...),
	}
}

// UserAccountWaitCriterion a function to check that a user account has the expected condition
type UserAccountWaitCriterion func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool

// UntilUserAccountHasSpec returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount has the expected spec
func UntilUserAccountHasSpec(expected toolchainv1alpha1.UserAccountSpec) UserAccountWaitCriterion {
	return func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool {
		a.T.Logf("waiting for useraccount specs. Actual: '%+v'; Expected: '%+v'", ua.Spec, expected)
		userAccount := ua.DeepCopy()
		userAccount.Spec.NSTemplateSet = toolchainv1alpha1.NSTemplateSetSpec{}
		expectedSpec := expected.DeepCopy()
		expectedSpec.NSTemplateSet = toolchainv1alpha1.NSTemplateSetSpec{}
		return reflect.DeepEqual(userAccount.Spec, *expectedSpec)
	}
}

// UntilUserAccountMatchesMur returns a `UserAccountWaitCriterion` which loads the existing MUR
// and compares the first UserAccountSpecEmbedded in the MUR with the actual UserAccount spec
func UntilUserAccountMatchesMur(hostAwaitility *HostAwaitility) UserAccountWaitCriterion {
	return func(a *MemberAwaitility, ua *toolchainv1alpha1.UserAccount) bool {
		mur, err := hostAwaitility.GetMasterUserRecord(WithMurName(ua.Name))
		if err != nil {
			a.T.Logf("error while getting MUR: %s", err)
			return false
		}
		a.T.Logf("comparing UserAccount '%s' vs MasterUserRecord '%s:"+
			"\nUserAccountSpecBase specs: '%+v' vs '%+v',"+
			"\nUserID: '%+v' vs '%+v'"+
			"\nDisabled: '%+v' vs '%+v'",
			ua.Name,
			mur.Name,
			ua.Spec.UserAccountSpecBase,
			mur.Spec.UserAccounts[0].Spec.UserAccountSpecBase,
			ua.Spec.UserID,
			mur.Spec.UserID,
			ua.Spec.Disabled,
			mur.Spec.Disabled)
		return ua.Spec.UserID == mur.Spec.UserID &&
			ua.Spec.Disabled == mur.Spec.Disabled &&
			reflect.DeepEqual(ua.Spec.UserAccountSpecBase, mur.Spec.UserAccounts[0].Spec.UserAccountSpecBase)
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
		a.T.Logf("waiting for status condition of UserSignup '%s'. Actual: '%+v'; Expected: '%+v'", ua.Name, ua.Status.Conditions, conditions)
		return false
	}
}

// WaitForUserAccount waits until there is a UserAccount available with the given name, expected spec and the set of status conditions
func (a *MemberAwaitility) WaitForUserAccount(name string, criteria ...UserAccountWaitCriterion) (*toolchainv1alpha1.UserAccount, error) {
	var userAccount *toolchainv1alpha1.UserAccount
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
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
		a.T.Logf("waiting for status condition of NSTemplateSet '%s'. Actual: '%+v'; Expected: '%+v'", nsTmplSet.Name, nsTmplSet.Status.Conditions, conditions)
		return false
	}
}

// UntilNSTemplateSetHasTier checks if the NSTemplateTier has the expected tierName
func UntilNSTemplateSetHasTier(tier string) NSTemplateSetWaitCriterion {
	return func(a *MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet) bool {
		if nsTmplSet.Spec.TierName == tier {
			a.T.Logf("tierName in NSTemplateSet '%s` matches the expected tier", nsTmplSet.Name)
			return true
		}
		a.T.Logf("waiting for NSTemplateSet '%s' having the expected tierName. Actual: '%s'; Expected: '%s'", nsTmplSet.Name, nsTmplSet.Spec.TierName, tier)
		return false
	}
}

// WaitForNSTmplSet wait until the NSTemplateSet with the given name and conditions exists
func (a *MemberAwaitility) WaitForNSTmplSet(name string, criteria ...NSTemplateSetWaitCriterion) (*toolchainv1alpha1.NSTemplateSet, error) {
	var nsTmplSet *toolchainv1alpha1.NSTemplateSet
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, obj); err != nil {
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
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, nsTmplSet); err != nil {
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

// WaitForNamespace waits until a namespace with the given owner (username), type, revision and tier labels exists
func (a *MemberAwaitility) WaitForNamespace(username, ref string) (*v1.Namespace, error) {
	namespaceList := &v1.NamespaceList{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		namespaceList = &v1.NamespaceList{}
		tier, kind, _, err := Split(ref)
		if err != nil {
			return false, err
		}
		labels := map[string]string{
			"toolchain.dev.openshift.com/owner":       username,
			"toolchain.dev.openshift.com/templateref": ref,
			"toolchain.dev.openshift.com/tier":        tier,
			"toolchain.dev.openshift.com/type":        kind,
			"toolchain.dev.openshift.com/provider":    "codeready-toolchain",
		}
		opts := client.MatchingLabels(labels)
		if err := a.Client.List(context.TODO(), namespaceList, opts); err != nil {
			return false, err
		}

		// no match found, so we display the current list of namespaces
		if len(namespaceList.Items) == 0 {
			allNSs := &v1.NamespaceList{}
			ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
			if err := a.Client.List(context.TODO(), allNSs, client.MatchingLabels(ls)); err != nil {
				return false, err
			}
			allNSNames := make(map[string]map[string]string, len(allNSs.Items))
			for _, ns := range allNSs.Items {
				allNSNames[ns.Name] = ns.Labels
			}
			a.T.Logf("waiting for availability of namespace with templateRef '%s' and owned by '%s'. Currently available codeready-toolchain NSs: '%+v'", ref, username, allNSNames)
			return false, nil
		}
		require.Len(a.T, namespaceList.Items, 1, "there should be only one Namespace found")
		// exclude namespace if it's not `Active` phase
		ns := namespaceList.Items[0]
		if ns.Status.Phase != v1.NamespaceActive {
			a.T.Logf("waiting for namespace with templateRef '%s' and owned by '%s' to be in 'Active' phase. Current phase: '%s'", ref, username, ns.Status.Phase)
			return false, nil
		}
		a.T.Logf("found Namespace with templateRef '%s'", ref)
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
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
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

// WaitForLimitRange waits until a LimitRange with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForLimitRange(namespace *v1.Namespace, name string) (*v1.LimitRange, error) {
	lr := &v1.LimitRange{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &v1.LimitRange{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allLRs := &v1.LimitRangeList{}
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), allLRs, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of LimitRange '%s' in namespace '%s'. Currently available codeready-toolchain LimitRanges: '%+v'", name, namespace.Name, allLRs)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found LimitRange '%s'", name)
		lr = obj
		return true, nil
	})
	return lr, err
}

// WaitForNetworkPolicy waits until a NetworkPolicy with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForNetworkPolicy(namespace *v1.Namespace, name string) (*netv1.NetworkPolicy, error) {
	np := &netv1.NetworkPolicy{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &netv1.NetworkPolicy{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allNPs := &netv1.NetworkPolicyList{}
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), allNPs, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of NetworkPolicy '%s' in namespace '%s'. Currently available codeready-toolchain NetworkPolicies: '%+v'", name, namespace.Name, allNPs)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found NetworkPolicy '%s'", name)
		np = obj
		return true, nil
	})
	return np, err
}

// WaitForRole waits until a Role with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForRole(namespace *v1.Namespace, name string) (*rbacv1.Role, error) {
	role := &rbacv1.Role{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &rbacv1.Role{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allRoles := &rbacv1.RoleList{}
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
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

// WaitForClusterResourceQuota waits until a ClusterResourceQuota with the given name exists
func (a *MemberAwaitility) WaitForClusterResourceQuota(name string) (*quotav1.ClusterResourceQuota, error) {
	quota := &quotav1.ClusterResourceQuota{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &quotav1.ClusterResourceQuota{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				quotaList := &quotav1.ClusterResourceQuotaList{}
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), quotaList, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of ClusterResourceQuota '%s'. Currently available codeready-toolchain ClusterResourceQuotas: '%+v'", name, quotaList)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found ClusterResourceQuota '%s'", name)
		quota = obj
		return true, nil
	})
	return quota, err
}

// IdlerWaitCriterion a function to check that an Idler has the expected condition
type IdlerWaitCriterion func(a *MemberAwaitility, idler toolchainv1alpha1.Idler) bool

// IdlerConditions returns a `IdlerWaitCriterion` which checks that the given
// Idler has exactly all the given status conditions
func IdlerConditions(conditions ...toolchainv1alpha1.Condition) IdlerWaitCriterion {
	return func(a *MemberAwaitility, idler toolchainv1alpha1.Idler) bool {
		if test.ConditionsMatch(idler.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in Idler '%s'", idler.Name)
			return true
		}
		a.T.Logf("waiting for status condition of Idler '%s'. Actual: '%+v'; Expected: '%+v'", idler.Name, idler.Status.Conditions, conditions)
		return false
	}
}

// WaitForIdler waits until an Idler with the given name exists
func (a *MemberAwaitility) WaitForIdler(name string, criteria ...IdlerWaitCriterion) (*toolchainv1alpha1.Idler, error) {
	idler := &toolchainv1alpha1.Idler{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Idler{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				idlerList := &toolchainv1alpha1.IdlerList{}
				ls := map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"}
				if err := a.Client.List(context.TODO(), idlerList, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				a.T.Logf("waiting for availability of Idler '%s'. Currently available codeready-toolchain Idlers: '%+v'", name, idlerList)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, *obj) {
				return false, nil
			}
		}
		a.T.Logf("found Idler '%s'", name)
		idler = obj
		return true, nil
	})
	return idler, err
}

// UpdateIdlerSpec tries to update the Idler.Spec until success
func (a *MemberAwaitility) UpdateIdlerSpec(idler *toolchainv1alpha1.Idler) (*toolchainv1alpha1.Idler, error) {
	result := &toolchainv1alpha1.Idler{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Idler{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: idler.Name}, obj); err != nil {
			return false, err
		}
		obj.Spec = idler.Spec
		if err := a.Client.Update(context.TODO(), obj); err != nil {
			a.T.Logf("trying to update Idler %s. Error: %s. Will try to update again.", idler.Name, err.Error())
			return false, nil
		}
		result = obj
		return true, nil
	})
	return result, err
}

// Create tries to create the object until success
// Workaround for https://github.com/kubernetes/kubernetes/issues/67761
func (a *MemberAwaitility) Create(obj runtime.Object) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Create(context.TODO(), obj); err != nil {
			a.T.Logf("trying to create %+v. Error: %s. Will try to create again.", obj, err.Error())
			return false, nil
		}
		return true, nil
	})
	return err
}

// PodWaitCriterion a function to check that a Pod has the expected condition
type PodWaitCriterion func(a *MemberAwaitility, pod v1.Pod) bool

// WaitForPods waits until "n" number of pods exist in the given namespace
func (a *MemberAwaitility) WaitForPods(namespace string, n int, criteria ...PodWaitCriterion) ([]v1.Pod, error) {
	pods := make([]v1.Pod, 0, n)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		pds := make([]v1.Pod, 0, n)
		foundPods := &v1.PodList{}
		if err := a.Client.List(context.TODO(), foundPods, client.InNamespace(namespace)); err != nil {
			return false, err
		}
	pods:
		for _, p := range foundPods.Items {
			for _, match := range criteria {
				if !match(a, p) {
					// skip as soon as one criterion does not match
					continue pods
				}
			}
			pod := p // copy
			pds = append(pds, pod)
		}
		if len(pds) != n {
			a.T.Logf("waiting for %d pods with criterions '%v' in namespace '%s'. Currently available pods: '%s'", n, criteria, namespace, a.listPods(*foundPods))
			return false, nil
		}
		a.T.Logf("found Pods: '%s'", a.listPodsAsArray(pds))
		pods = pds
		return true, nil
	})
	a.T.Logf("found %d pods", len(pods))
	return pods, err
}

func (a *MemberAwaitility) listPods(pods v1.PodList) string {
	return a.listPodsAsArray(pods.Items)
}

func (a *MemberAwaitility) listPodsAsArray(pods []v1.Pod) string {
	var s string
	for _, p := range pods {
		s = fmt.Sprintf("%s\n%s", s, a.formatPod(p))
	}
	return s
}

func (a *MemberAwaitility) formatPod(pod v1.Pod) string {
	return fmt.Sprintf("Name: %s; Namespace: %s; Labels: %v; Phase: %s", pod.Name, pod.Namespace, pod.Labels, pod.Status.Phase)
}

// WaitUntilPodsDeleted waits until the pods are deleted from the given namespace
func (a *MemberAwaitility) WaitUntilPodsDeleted(namespace string, criteria ...PodWaitCriterion) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		foundPods := &v1.PodList{}
		if err := a.Client.List(context.TODO(), foundPods, &client.ListOptions{Namespace: namespace}); err != nil {
			return false, err
		}
		if len(foundPods.Items) == 0 {
			return true, nil
		}
		for _, p := range foundPods.Items {
			for _, match := range criteria {
				if !match(a, p) {
					a.T.Logf("waiting for pods in namespace %s with a specific criterion to be deleted. Found pod which matches the criterion: '%s'. All available pods: '%s'", namespace, a.formatPod(p), a.listPods(*foundPods))
					return false, nil
				}
			}
		}
		return true, nil
	})
	return err
}

// WaitUntilPodDeleted waits until the pod with the given name is deleted from the given namespace
func (a *MemberAwaitility) WaitUntilPodDeleted(namespace, name string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &v1.Pod{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("deleted Pod with name %s", name)
				return true, nil
			}
			return false, err
		}
		a.T.Logf("waiting for deletion of Pod with name '%s' in namespace %s", name, namespace)
		return false, nil
	})
}

// PodRunning checks if the Pod in the running phase
func PodRunning() PodWaitCriterion {
	return func(a *MemberAwaitility, pod v1.Pod) bool {
		if pod.Status.Phase == v1.PodRunning {
			a.T.Logf("pod '%s` in the running phase", pod.Name)
			return true
		}
		a.T.Logf("Pod '%s' actual phase: '%s'; Expected: '%s'", pod.Name, pod.Status.Phase, v1.PodRunning)
		return false
	}
}

// WithPodName checks if the Pod has the expected name
func WithPodName(name string) PodWaitCriterion {
	return func(a *MemberAwaitility, pod v1.Pod) bool {
		return pod.Name == name
	}
}

// WithPodLabel checks if the Pod has the expected label
func WithPodLabel(key, value string) PodWaitCriterion {
	return func(a *MemberAwaitility, pod v1.Pod) bool {
		return pod.Labels[key] == value
	}
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
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, ua); err != nil {
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

// WaitUntilClusterResourceQuotasDeleted waits until all ClusterResourceQuotas with the given owner label are deleted (ie, none is found)
func (a *MemberAwaitility) WaitUntilClusterResourceQuotasDeleted(username string) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{"toolchain.dev.openshift.com/owner": username}
		opts := client.MatchingLabels(labels)
		quotaList := &quotav1.ClusterResourceQuotaList{}
		if err := a.Client.List(context.TODO(), quotaList, opts); err != nil {
			return false, err
		}
		if len(quotaList.Items) == 0 {
			a.T.Logf("deleted all ClusterResourceQuotas with the owner label '%s'", username)
			return true, nil
		}
		a.T.Logf("waiting for deletion of the following ClusterResourceQuotas '%v'", quotaList.Items)
		return false, nil
	})
}

// MemberStatusWaitCriterion a function to check that an MemberStatus has the expected condition
type MemberStatusWaitCriterion func(*MemberAwaitility, *toolchainv1alpha1.MemberStatus) bool

// UntilMemberStatusHasConditions returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has exactly all the given status conditions
func UntilMemberStatusHasConditions(conditions ...toolchainv1alpha1.Condition) MemberStatusWaitCriterion {
	return func(a *MemberAwaitility, memberStatus *toolchainv1alpha1.MemberStatus) bool {
		if test.ConditionsMatch(memberStatus.Status.Conditions, conditions...) {
			a.T.Logf("status conditions match in MemberStatus '%s'", memberStatus.Name)
			return true
		}
		a.T.Logf("waiting for status condition of MemberStatus '%s'. Actual: '%+v'; Expected: '%+v'", memberStatus.Name, memberStatus.Status.Conditions, conditions)
		return false
	}
}

// UntilMemberStatusHasUsageSet returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has some non-zero resource usage set
func UntilMemberStatusHasUsageSet() MemberStatusWaitCriterion {
	return func(a *MemberAwaitility, memberStatus *toolchainv1alpha1.MemberStatus) bool {
		return hasMemberStatusUsageSet(a.T, memberStatus.Name, memberStatus.Status)
	}
}

func hasMemberStatusUsageSet(t *testing.T, name string, status toolchainv1alpha1.MemberStatusStatus) bool {
	usage := status.ResourceUsage.MemoryUsagePerNodeRole
	if len(usage) == 2 && usage["worker"] > 0 && usage["master"] > 0 {
		t.Logf("the MemberStatus '%s' has resource usage set for both worker and master nodes: %v", name, usage)
		return true
	}
	t.Logf("the MemberStatus '%s' doesn't have resource usage set for both worker and master nodes, actual: %v", name, usage)
	return false
}

// WaitForMemberStatus waits until the MemberStatus is available with the provided criteria, if any
func (a *MemberAwaitility) WaitForMemberStatus(criteria ...MemberStatusWaitCriterion) error {
	// there should only be one member status with the name toolchain-member-status
	name := "toolchain-member-status"
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		// retrieve the memberstatus from the member namespace
		memberStatus := toolchainv1alpha1.MemberStatus{}
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			&memberStatus)
		if err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for availability of memberstatus '%s' in namespace '%s'...\n", name, a.Namespace)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, &memberStatus) {
				return false, nil
			}
		}
		a.T.Logf("found memberstatus '%s': %+v", memberStatus.Name, memberStatus)
		return true, nil
	})
	return err
}

// DeleteUserAccount deletes the user account resource with the given name and
// waits until it was actually deleted
func (a *MemberAwaitility) DeleteUserAccount(name string) error {
	ua, err := a.WaitForUserAccount(name)
	if err != nil {
		return err
	}
	if err = a.Client.Delete(context.TODO(), ua); err != nil {
		return err
	}
	return a.WaitUntilUserAccountDeleted(name)

}
