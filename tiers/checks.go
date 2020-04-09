package tiers

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	providerMatchingLabels = client.MatchingLabels(map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"})
	commonChecks           = []innerObjectCheck{
		userEditRoleBinding(),
		networkPolicySameNamespace(),
		networkPolicyAllowFromMonitoring(),
		networkPolicyAllowFromIngress(),
		numberOfLimitRanges(1),
		numberOfNetworkPolicies(3),
	}
)

func NewChecks(tier string) (TierChecks, error) {
	switch tier {
	case "basic":
		return &basicTierChecks{}, nil

	case "advanced":
		return &advancedTierChecks{}, nil

	case "team":
		return &teamTierChecks{}, nil

	default:
		return nil, fmt.Errorf("no assertion implementation found for %s", tier)
	}
}

type TierChecks interface {
	GetInnerObjectChecks(nsType string) []innerObjectCheck
	GetExpectedRevisions(awaitility *wait.Awaitility) Revisions
}

type basicTierChecks struct {
}

func (a *basicTierChecks) GetInnerObjectChecks(nsType string) []innerObjectCheck {
	return getDefaultChecks(nsType)
}

func getDefaultChecks(nsType string) []innerObjectCheck {
	cpuLimit := "150m"
	memoryLimit := "512Mi"
	if nsType == "dev" {
		cpuLimit = "250m"
		memoryLimit = "1Gi"
	}
	defaultCommonChecks := append(commonChecks, limitRange(cpuLimit, memoryLimit))
	if nsType == "code" {
		return append(defaultCommonChecks,
			rbacEditRoleBinding(),
			rbacEditRole(),
			numberOfToolchainRoles(1),
			numberOfToolchainRoleBindings(2))
	}
	return append(defaultCommonChecks,
		numberOfToolchainRoles(0),
		numberOfToolchainRoleBindings(1))
}

func (a *basicTierChecks) GetExpectedRevisions(awaitility *wait.Awaitility) Revisions {
	revisions := GetRevisions(awaitility, "basic", "code", "dev", "stage")
	return revisions
}

type advancedTierChecks struct {
}

func (a *advancedTierChecks) GetInnerObjectChecks(nsType string) []innerObjectCheck {
	return getDefaultChecks(nsType)
}

func (a *advancedTierChecks) GetExpectedRevisions(awaitility *wait.Awaitility) Revisions {
	revisions := GetRevisions(awaitility, "advanced", "code", "dev", "stage")
	return revisions
}

type teamTierChecks struct {
}

func (a *teamTierChecks) GetInnerObjectChecks(nsType string) []innerObjectCheck {
	return append(commonChecks,
		limitRange("500m", "2Gi"),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2),
	)
}

func (a *teamTierChecks) GetExpectedRevisions(awaitility *wait.Awaitility) Revisions {
	revisions := GetRevisions(awaitility, "team", "dev", "stage")
	return revisions
}

type innerObjectCheck func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string)

func userEditRoleBinding() innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, "user-edit")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "edit", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func rbacEditRoleBinding() innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, "user-rbac-edit")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "rbac-edit", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func rbacEditRole() innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(ns, "rbac-edit")
		require.NoError(t, err)
		assert.Len(t, role.Rules, 1)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"authorization.openshift.io", "rbac.authorization.k8s.io"},
					Resources: []string{"roles", "rolebindings"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func limitRange(cpuLimit, memoryLimit string) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		lr, err := memberAwait.WaitForLimitRange(ns, "resource-limits")
		require.NoError(t, err)
		def := make(map[v1.ResourceName]resource.Quantity)
		def[corev1.ResourceCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		def[corev1.ResourceMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		defReq := make(map[v1.ResourceName]resource.Quantity)
		defReq[corev1.ResourceCPU], err = resource.ParseQuantity("100m")
		require.NoError(t, err)
		defReq[corev1.ResourceMemory], err = resource.ParseQuantity("64Mi")
		require.NoError(t, err)
		assert.Equal(t, "codeready-toolchain", lr.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		expected := &v1.LimitRange{
			Spec: v1.LimitRangeSpec{
				Limits: []v1.LimitRangeItem{
					{
						Type:           "Container",
						Default:        def,
						DefaultRequest: defReq,
					},
				},
			},
		}

		assert.Equal(t, expected.Spec, lr.Spec)
	}
}

func networkPolicySameNamespace() innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		np, err := memberAwait.WaitForNetworkPolicy(ns, "allow-same-namespace")
		require.NoError(t, err)
		assert.Equal(t, "codeready-toolchain", np.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		expected := &netv1.NetworkPolicy{
			Spec: netv1.NetworkPolicySpec{
				Ingress: []netv1.NetworkPolicyIngressRule{
					{
						From: []netv1.NetworkPolicyPeer{
							{
								PodSelector: &metav1.LabelSelector{},
							},
						},
					},
				},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
			},
		}

		assert.Equal(t, expected.Spec, np.Spec)
	}
}

func networkPolicyAllowFromIngress() innerObjectCheck {
	return networkPolicyIngress("allow-from-openshift-ingress", "ingress")
}

func networkPolicyAllowFromMonitoring() innerObjectCheck {
	return networkPolicyIngress("allow-from-openshift-monitoring", "monitoring")
}

func networkPolicyIngress(name, group string) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		np, err := memberAwait.WaitForNetworkPolicy(ns, name)
		require.NoError(t, err)
		assert.Equal(t, "codeready-toolchain", np.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		expected := &netv1.NetworkPolicy{
			Spec: netv1.NetworkPolicySpec{
				Ingress: []netv1.NetworkPolicyIngressRule{
					{
						From: []netv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"network.openshift.io/policy-group": group}},
							},
						},
					},
				},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
			},
		}

		assert.Equal(t, expected.Spec, np.Spec)
	}
}

func numberOfToolchainRoles(number int) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		roles := &rbacv1.RoleList{}
		err := memberAwait.Client.List(context.TODO(), roles, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, roles.Items, number)
	}
}

func numberOfToolchainRoleBindings(number int) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		roleBindings := &rbacv1.RoleBindingList{}
		err := memberAwait.Client.List(context.TODO(), roleBindings, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, roleBindings.Items, number)
	}
}

func numberOfLimitRanges(number int) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		limitRanges := &v1.LimitRangeList{}
		err := memberAwait.Client.List(context.TODO(), limitRanges, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, limitRanges.Items, number)
	}
}

func numberOfNetworkPolicies(number int) innerObjectCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		nps := &netv1.NetworkPolicyList{}
		err := memberAwait.Client.List(context.TODO(), nps, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, nps.Items, number)
	}
}
