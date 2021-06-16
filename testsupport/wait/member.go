package wait

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/codeready-toolchain/toolchain-common/pkg/status"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"

	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
)

var (
	appMemberOperatorWebhookLabel = map[string]string{
		"app": "member-operator-webhook",
	}
	codereadyToolchainProviderLabel = map[string]string{
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}
	bothWebhookLabels = map[string]string{
		"app":                                  "member-operator-webhook",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}
)

type MemberAwaitility struct {
	*Awaitility
}

func NewMemberAwaitility(t *testing.T, cl client.Client, ns, clusterName string) *MemberAwaitility {
	return &MemberAwaitility{
		Awaitility: &Awaitility{
			Client:        cl,
			ClusterName:   clusterName,
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
				a.T.Logf("waiting for availability of useraccount '%s' in namespace '%s'", name, a.Namespace)
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
			if err := a.Client.List(context.TODO(), allNSs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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
				if err := a.Client.List(context.TODO(), allRBs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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
				if err := a.Client.List(context.TODO(), allLRs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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
				if err := a.Client.List(context.TODO(), allNPs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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
				if err := a.Client.List(context.TODO(), allRoles, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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

// ClusterResourceQuotaWaitCriterion a function to check that an ClusterResourceQuota has the expected criteria
type ClusterResourceQuotaWaitCriterion func(a *MemberAwaitility, quota quotav1.ClusterResourceQuota) bool

// WaitForClusterResourceQuota waits until a ClusterResourceQuota with the given name exists
func (a *MemberAwaitility) WaitForClusterResourceQuota(name string, criteria ...ClusterResourceQuotaWaitCriterion) (*quotav1.ClusterResourceQuota, error) {
	quota := &quotav1.ClusterResourceQuota{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &quotav1.ClusterResourceQuota{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				quotaList := &quotav1.ClusterResourceQuotaList{}
				ls := codereadyToolchainProviderLabel
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

		for _, match := range criteria {
			if !match(a, *obj) {
				return false, nil
			}
		}

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

// IdlerHasTier checks if the Idler has the given tier name set as a label
func IdlerHasTier(tierName string) IdlerWaitCriterion {
	return func(a *MemberAwaitility, idler toolchainv1alpha1.Idler) bool {
		if idler.Labels != nil && tierName == idler.Labels["toolchain.dev.openshift.com/tier"] {
			a.T.Logf("tier label '%s' matches in Idler '%s'", tierName, idler.Name)
			return true
		}
		a.T.Logf("waiting for Idler '%s' to match the tier label. Actual labels: '%v'; Expected: '%s'", idler.Name, idler.Labels, tierName)
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
				if err := a.Client.List(context.TODO(), idlerList, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
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

// WaitForPod waits until a pod with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForPod(namespace, name string, criteria ...PodWaitCriterion) (v1.Pod, error) {
	pod := v1.Pod{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err = a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, &pod); err != nil {
			if errors.IsNotFound(err) {
				// loop again
				return false, nil
			}
			// exit
			return false, err
		}
		for _, match := range criteria {
			if !match(a, pod) {
				// skip as soon as one criterion does not match
				return false, nil
			}
		}
		a.T.Logf("found Pod '%s/%s'", pod.Namespace, pod.Name)
		return true, nil
	})
	return pod, err
}

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

func WithSandboxPriorityClass() PodWaitCriterion {
	return func(a *MemberAwaitility, pod v1.Pod) bool {
		return checkPriorityClass(a, pod, "sandbox-users-pods", -10)
	}
}

func WithOriginalPriorityClass() PodWaitCriterion {
	return func(a *MemberAwaitility, pod v1.Pod) bool {
		if pod.Name != "idler-test-pod-1" {
			return checkPriorityClass(a, pod, "", 0)
		}
		return checkPriorityClass(a, pod, "system-cluster-critical", 2000000000)
	}
}

func checkPriorityClass(a *MemberAwaitility, pod v1.Pod, name string, priority int) bool {
	if pod.Spec.PriorityClassName == name && *pod.Spec.Priority == int32(priority) {
		return true
	}
	a.T.Logf("Priority doesn't match for pod '%s' - Expected prorityClassName: '%s', actual : '%s'; Expected prority: '%d', actual : '%d'", pod.Name, name, pod.Spec.PriorityClassName, priority, *pod.Spec.Priority)
	return false
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

// GetConsoleURL retrieves Web Console Route and returns its URL
func (a *MemberAwaitility) GetConsoleURL() string {
	route := &routev1.Route{}
	namespacedName := types.NamespacedName{Namespace: "openshift-console", Name: "console"}
	err := a.Client.Get(context.TODO(), namespacedName, route)
	require.NoError(a.T, err)
	return fmt.Sprintf("https://%s/%s", route.Spec.Host, route.Spec.Path)
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

// UntilMemberStatusHasConsoleURLSet returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has a non-empty console url set
func UntilMemberStatusHasConsoleURLSet(expectedURL string, condition toolchainv1alpha1.Condition) MemberStatusWaitCriterion {
	return func(awaitility *MemberAwaitility, status *toolchainv1alpha1.MemberStatus) bool {
		return hasMemberStatusConsoleURLSet(awaitility, expectedURL, condition, status.Name, status.Status)
	}
}

func hasMemberStatusConsoleURLSet(a *MemberAwaitility, expectedURL string, condition toolchainv1alpha1.Condition, name string, memberStatus toolchainv1alpha1.MemberStatusStatus) bool {
	if memberStatus.Routes == nil {
		a.T.Logf("waiting for routes to be set in MemberStatus '%s'. Actual: '%+v'", name, memberStatus.Routes)
		return false
	}
	if memberStatus.Routes.ConsoleURL != expectedURL {
		a.T.Logf("waiting for console route to be set in MemberStatus '%s'. Actual: '%+v'; Expected: '%+v'", name, *memberStatus.Routes, expectedURL)
		return false
	}
	if !test.ConditionsMatch(memberStatus.Routes.Conditions, condition) {
		a.T.Logf("waiting for routes condition of MemberStatus '%s'. Actual: '%+v'; Expected: '%+v'", name, memberStatus.Routes.Conditions, condition)
		return false
	}
	a.T.Logf("console route and condition is properly set in MemberStatus '%s'", name)
	return true
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

// GetMemberOperatorConfig returns MemberOperatorConfig instance, nil if not found
func (a *MemberAwaitility) GetMemberOperatorConfig() *toolchainv1alpha1.MemberOperatorConfig {
	config := &toolchainv1alpha1.MemberOperatorConfig{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, "config"), config); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		require.NoError(a.T, err)
	}
	return config
}

// MemberOperatorConfigWaitCriterion a function to check that an MemberOperatorConfig has the expected criteria
type MemberOperatorConfigWaitCriterion func(*MemberAwaitility, *toolchainv1alpha1.MemberOperatorConfig) bool

// UntilMemberConfigMatches returns a `MemberOperatorConfigWaitCriterion` which checks that the given
// MemberOperatorConfig matches the provided one
func UntilMemberConfigMatches(expectedMemberOperatorConfig *toolchainv1alpha1.MemberOperatorConfig) MemberOperatorConfigWaitCriterion {
	return func(a *MemberAwaitility, memberConfig *toolchainv1alpha1.MemberOperatorConfig) bool {
		if reflect.DeepEqual(expectedMemberOperatorConfig.Spec, memberConfig.Spec) {
			return true
		}
		a.T.Logf("waiting for MemberOperatorConfig to be synced. Actual: '%+v'; Expected: '%+v'", memberConfig, expectedMemberOperatorConfig)
		return false
	}
}

// WaitForMemberOperatorConfig waits until the MemberOperatorConfig is available with the provided criteria, if any
func (a *MemberAwaitility) WaitForMemberOperatorConfig(criteria ...MemberOperatorConfigWaitCriterion) (*toolchainv1alpha1.MemberOperatorConfig, error) {
	// there should only be one MemberOperatorConfig with the name config
	name := "config"
	memberOperatorConfig := &toolchainv1alpha1.MemberOperatorConfig{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		memberOperatorConfig = &toolchainv1alpha1.MemberOperatorConfig{}
		// retrieve the MemberOperatorConfig from the member namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			memberOperatorConfig)
		if err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for availability of MemberOperatorConfig '%s' in namespace '%s'...\n", name, a.Namespace)
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(a, memberOperatorConfig) {
				return false, nil
			}
		}
		a.T.Logf("found memberOperatorConfig '%s': %+v", memberOperatorConfig.Name, memberOperatorConfig)
		return true, nil
	})
	return memberOperatorConfig, err
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

// GetMemberOperatorPod returns the pod running the member operator controllers
func (a *MemberAwaitility) GetMemberOperatorPod() (corev1.Pod, error) {
	pods := corev1.PodList{}
	if err := a.Client.List(context.TODO(), &pods, client.InNamespace(a.Namespace), client.MatchingLabels{"name": "member-operator"}); err != nil {
		return corev1.Pod{}, err
	}
	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("unexpected number of pods with label 'name=member-operator' in namespace '%s': %d ", a.Namespace, len(pods.Items))
	}
	return pods.Items[0], nil
}

func (a *MemberAwaitility) WaitForUsersPodsWebhook() {
	a.waitForUsersPodPriorityClass()
	a.waitForService()
	a.waitForWebhookDeployment()
	ca := a.waitForSecret()
	a.waitForWebhookConfig(ca)
}

func (a *MemberAwaitility) waitForUsersPodPriorityClass() {
	a.T.Logf("checking prensence of PrioritiyClass resource '%s'", "sandbox-users-pods")
	actualPrioClass := &schedulingv1.PriorityClass{}
	a.waitForResource("", "sandbox-users-pods", actualPrioClass)

	assert.Equal(a.T, codereadyToolchainProviderLabel, actualPrioClass.Labels)
	assert.Equal(a.T, int32(-10), actualPrioClass.Value)
	assert.False(a.T, actualPrioClass.GlobalDefault)
	assert.Equal(a.T, "Priority class for pods in users' namespaces", actualPrioClass.Description)
}

func (a *MemberAwaitility) waitForResource(namespace, name string, object runtime.Object) {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), test.NamespacedName(namespace, name), object); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("resource '%s' in namesapace '%s' not found", name, namespace)
				return false, nil
			}
			a.T.Logf("unexpected error when looking for resource '%s' in namesapace '%s'", name, namespace)
			return false, err
		}
		a.T.Logf("resource '%s' in namesapace '%s' found", name, namespace)
		return true, nil
	})
	require.NoError(a.T, err)
}

func (a *MemberAwaitility) waitForService() {
	a.T.Logf("checking prensence of Service resource '%s' in namesapace '%s'", "member-operator-webhook", a.Namespace)
	actualService := &v1.Service{}
	a.waitForResource(a.Namespace, "member-operator-webhook", actualService)

	assert.Equal(a.T, map[string]string{
		"app":                                  "member-operator-webhook",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}, actualService.Labels)
	require.Len(a.T, actualService.Spec.Ports, 1)
	assert.Equal(a.T, int32(443), actualService.Spec.Ports[0].Port)
	assert.Equal(a.T, intstr.IntOrString{
		IntVal: 8443,
	}, actualService.Spec.Ports[0].TargetPort)
	assert.Equal(a.T, appMemberOperatorWebhookLabel, actualService.Spec.Selector)
}

func (a *MemberAwaitility) waitForWebhookDeployment() {
	a.T.Logf("checking prensence of Deployment resource '%s' in namesapace '%s'", "member-operator-webhook", a.Namespace)
	actualDeployment := &appsv1.Deployment{}
	a.waitForResource(a.Namespace, "member-operator-webhook", actualDeployment)

	assert.Equal(a.T, bothWebhookLabels, actualDeployment.Labels)
	assert.Equal(a.T, int32(1), *actualDeployment.Spec.Replicas)
	assert.Equal(a.T, appMemberOperatorWebhookLabel, actualDeployment.Spec.Selector.MatchLabels)

	template := actualDeployment.Spec.Template
	assert.Equal(a.T, "member-operator-webhook", template.ObjectMeta.Name)
	assert.Equal(a.T, appMemberOperatorWebhookLabel, template.ObjectMeta.Labels)
	require.Len(a.T, template.Spec.Volumes, 1)
	assert.Equal(a.T, "webhook-certs", template.Spec.Volumes[0].Name)
	assert.Equal(a.T, "webhook-certs", template.Spec.Volumes[0].Secret.SecretName)
	require.Len(a.T, template.Spec.Containers, 1)

	container := template.Spec.Containers[0]
	assert.Equal(a.T, "mutator", container.Name)
	assert.NotEmpty(a.T, container.Image)
	assert.Equal(a.T, []string{"member-operator-webhook"}, container.Command)
	assert.Equal(a.T, v1.PullIfNotPresent, container.ImagePullPolicy)
	assert.NotEmpty(a.T, container.Resources)

	assert.Len(a.T, container.VolumeMounts, 1)
	assert.Equal(a.T, "webhook-certs", container.VolumeMounts[0].Name)
	assert.Equal(a.T, "/etc/webhook/certs", container.VolumeMounts[0].MountPath)
	assert.True(a.T, container.VolumeMounts[0].ReadOnly)

	a.waitForDeploymentToGetReady("member-operator-webhook")
}

func (a *MemberAwaitility) waitForSecret() []byte {
	a.T.Logf("checking presence of Secret resource '%s' in namesapace '%s'", "webhook-certs", a.Namespace)
	secret := &v1.Secret{}
	a.waitForResource(a.Namespace, "webhook-certs", secret)
	assert.NotEmpty(a.T, secret.Data["server-key.pem"])
	assert.NotEmpty(a.T, secret.Data["server-cert.pem"])
	ca := secret.Data["ca-cert.pem"]
	assert.NotEmpty(a.T, ca)
	return ca
}

func (a *MemberAwaitility) waitForWebhookConfig(ca []byte) {
	a.T.Logf("checking presence of MutatingWebhookConfiguration resource '%s'", "sandbox-users-pods")
	actualMutWbhConf := &admv1.MutatingWebhookConfiguration{}
	a.waitForResource("", "member-operator-webhook", actualMutWbhConf)
	assert.Equal(a.T, bothWebhookLabels, actualMutWbhConf.Labels)
	require.Len(a.T, actualMutWbhConf.Webhooks, 1)

	webhook := actualMutWbhConf.Webhooks[0]
	assert.Equal(a.T, "users.pods.webhook.sandbox", webhook.Name)
	assert.Equal(a.T, []string{"v1"}, webhook.AdmissionReviewVersions)
	assert.Equal(a.T, admv1.SideEffectClassNone, *webhook.SideEffects)
	assert.Equal(a.T, int32(5), *webhook.TimeoutSeconds)
	assert.Equal(a.T, admv1.NeverReinvocationPolicy, *webhook.ReinvocationPolicy)
	assert.Equal(a.T, admv1.Ignore, *webhook.FailurePolicy)
	assert.Equal(a.T, admv1.Equivalent, *webhook.MatchPolicy)
	assert.Equal(a.T, codereadyToolchainProviderLabel, webhook.NamespaceSelector.MatchLabels)
	assert.Equal(a.T, ca, webhook.ClientConfig.CABundle)
	assert.Equal(a.T, "member-operator-webhook", webhook.ClientConfig.Service.Name)
	assert.Equal(a.T, a.Namespace, webhook.ClientConfig.Service.Namespace)
	assert.Equal(a.T, "/mutate-users-pods", *webhook.ClientConfig.Service.Path)
	assert.Equal(a.T, int32(443), *webhook.ClientConfig.Service.Port)
	require.Len(a.T, webhook.Rules, 1)

	rule := webhook.Rules[0]
	assert.Equal(a.T, []admv1.OperationType{admv1.Create, admv1.Update}, rule.Operations)
	assert.Equal(a.T, []string{""}, rule.APIGroups)
	assert.Equal(a.T, []string{"v1"}, rule.APIVersions)
	assert.Equal(a.T, []string{"pods"}, rule.Resources)
	assert.Equal(a.T, admv1.NamespacedScope, *rule.Scope)
}

func (a *MemberAwaitility) WaitForAutoscalingBufferApp() {
	a.waitForAutoscalingBufferPriorityClass()
	a.waitForAutoscalingBufferDeployment()
}

func (a *MemberAwaitility) waitForAutoscalingBufferPriorityClass() {
	a.T.Logf("checking presence of PrioritiyClass resource '%s'", "member-operator-autoscaling-buffer")
	actualPrioClass := &schedulingv1.PriorityClass{}
	a.waitForResource("", "member-operator-autoscaling-buffer", actualPrioClass)

	assert.Equal(a.T, codereadyToolchainProviderLabel, actualPrioClass.Labels)
	assert.Equal(a.T, int32(-100), actualPrioClass.Value)
	assert.False(a.T, actualPrioClass.GlobalDefault)
	assert.Equal(a.T, "This priority class is to be used by the autoscaling buffer pod only", actualPrioClass.Description)
}

func (a *MemberAwaitility) waitForAutoscalingBufferDeployment() {
	a.T.Logf("checking prensence of Deployment resource '%s' in namesapace '%s'", "autoscaling-buffer", a.Namespace)
	actualDeployment := &appsv1.Deployment{}
	a.waitForResource(a.Namespace, "autoscaling-buffer", actualDeployment)

	assert.Equal(a.T, map[string]string{
		"app":                                  "autoscaling-buffer",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}, actualDeployment.Labels)
	assert.Equal(a.T, int32(2), *actualDeployment.Spec.Replicas)
	assert.Equal(a.T, map[string]string{"app": "autoscaling-buffer"}, actualDeployment.Spec.Selector.MatchLabels)

	template := actualDeployment.Spec.Template
	assert.Equal(a.T, map[string]string{"app": "autoscaling-buffer"}, template.ObjectMeta.Labels)

	assert.Equal(a.T, "member-operator-autoscaling-buffer", template.Spec.PriorityClassName)
	assert.Equal(a.T, int64(0), *template.Spec.TerminationGracePeriodSeconds)

	require.Len(a.T, template.Spec.Containers, 1)
	container := template.Spec.Containers[0]
	assert.Equal(a.T, "autoscaling-buffer", container.Name)
	assert.Equal(a.T, "gcr.io/google_containers/pause-amd64:3.2", container.Image)
	assert.Equal(a.T, v1.PullIfNotPresent, container.ImagePullPolicy)

	expectedMemory, err := resource.ParseQuantity("50Mi")
	require.NoError(a.T, err)
	assert.True(a.T, container.Resources.Requests.Memory().Equal(expectedMemory))
	assert.True(a.T, container.Resources.Limits.Memory().Equal(expectedMemory))

	a.waitForDeploymentToGetReady("autoscaling-buffer")
}

func (a *MemberAwaitility) waitForDeploymentToGetReady(name string) {
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		deploymentConditions := status.GetDeploymentStatusConditions(a.Client, name, a.Namespace)
		if err := status.ValidateComponentConditionReady(deploymentConditions...); err != nil {
			a.T.Logf("deployment '%s' in namesapace '%s' is not ready - current conditions: %v", name, a.Namespace, deploymentConditions)
			return false, nil
		}
		a.T.Logf("deployment '%s' in namesapace '%s' is ready", name, a.Namespace)
		return true, nil
	})
	require.NoError(a.T, err)
}

// WaitForExpectedNumberOfResources waits until the number of resources matches the expected count
func (a *MemberAwaitility) WaitForExpectedNumberOfResources(kind string, expected int, list func() (int, error)) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		actual, err := list()
		if err != nil {
			return false, err
		}
		if actual == expected {
			a.T.Logf("The number of %s resources matches", kind)
			return true, nil
		}
		a.T.Logf("Waiting for the expected number of %s resources. Actual: %d, Expected: %d", kind, actual, expected)
		return false, nil
	})
	return err
}
