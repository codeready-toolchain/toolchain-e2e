package tiers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/wait"

	quotav1 "github.com/openshift/api/quota/v1"
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
	commonChecks           = []namespaceObjectsCheck{
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

// Type splits the templateRef into a triple of string corresponding to the `tier`, `type` and `revision`
// returns an error if this TemplateRef's format is invalid
func split(templateRef string) (string, string, string, error) { // nolint: unparam
	parts := strings.Split(templateRef, "-") // "<tier>-<type>-<revision>"
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid templateref: '%v'", templateRef)
	}
	return parts[0], parts[1], parts[2], nil
}

type TierChecks interface {
	GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck
	GetClusterObjectChecks() []clusterObjectsCheck
	GetExpectedTemplateRefs(awaitility *wait.Awaitility) TemplateRefs
}

type basicTierChecks struct {
}

func (a *basicTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	defaultCommonChecks := append(commonChecks, a.limitRangeByType(nsType))
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

func (a *basicTierChecks) GetExpectedTemplateRefs(awaitility *wait.Awaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(awaitility.Host(), "basic")
	verifyNsTypes(awaitility.T, "basic", templateRefs, "code", "dev", "stage")
	return templateRefs
}

func (a *basicTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota("4000m", "1750m", "7Gi"),
		numberOfClusterResourceQuotas(1),
	}
}

func (a *basicTierChecks) limitRangeByType(nsType string) namespaceObjectsCheck {
	switch nsType {
	case "code":
		return limitRange("1", "512Mi", "60m", "307Mi")
	case "dev":
		return limitRange("150m", "750Mi", "10m", "64Mi")
	default:
		return limitRange("150m", "512Mi", "10m", "64Mi")
	}
}

type advancedTierChecks struct {
}

func (a *advancedTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	return append(commonChecks,
		a.limitRangeByType(nsType),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2))
}

func (a *advancedTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota("4000m", "1750m", "7Gi"),
		numberOfClusterResourceQuotas(1),
	}
}

func (a *advancedTierChecks) GetExpectedTemplateRefs(awaitility *wait.Awaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(awaitility.Host(), "advanced")
	verifyNsTypes(awaitility.T, "advanced", templateRefs, "code", "dev", "stage")
	return templateRefs
}

func (a *advancedTierChecks) limitRangeByType(nsType string) namespaceObjectsCheck {
	switch nsType {
	case "code":
		return limitRange("1", "512Mi", "60m", "307Mi")
	case "dev":
		return limitRange("150m", "750Mi", "10m", "64Mi")
	default:
		return limitRange("150m", "512Mi", "10m", "64Mi")
	}
}

type teamTierChecks struct {
}

func (a *teamTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	return append(commonChecks,
		limitRange("150m", "1Gi", "10m", "64Mi"),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2),
	)
}

func (a *teamTierChecks) GetExpectedTemplateRefs(awaitility *wait.Awaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(awaitility.Host(), "team")
	verifyNsTypes(awaitility.T, "team", templateRefs, "dev", "stage")
	return templateRefs
}

func (a *teamTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota("4000m", "2000m", "15Gi"),
		numberOfClusterResourceQuotas(1),
	}
}

// verifyNsTypes checks that there's a namespace.TemplateRef that begins with `<tier>-<type>` for each given templateRef (and no more, no less)
func verifyNsTypes(t *testing.T, tier string, templateRefs TemplateRefs, expectedNSTypes ...string) {
	require.Len(t, templateRefs.Namespaces, len(expectedNSTypes))
	actualNSTypes := make([]string, len(expectedNSTypes))
	for i, templateRef := range templateRefs.Namespaces {
		actualTier, actualType, _, err := split(templateRef)
		require.NoError(t, err)
		require.Equal(t, tier, actualTier)
		actualNSTypes[i] = actualType
	}
	// now compare with the given `nsTypes`
	assert.ElementsMatch(t, expectedNSTypes, actualNSTypes)
}

type namespaceObjectsCheck func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string)

type clusterObjectsCheck func(t *testing.T, memberAwait *wait.MemberAwaitility, userName string)

func userEditRoleBinding() namespaceObjectsCheck {
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

func rbacEditRoleBinding() namespaceObjectsCheck {
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

func rbacEditRole() namespaceObjectsCheck {
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

func limitRange(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		lr, err := memberAwait.WaitForLimitRange(ns, "resource-limits")
		require.NoError(t, err)
		def := make(map[v1.ResourceName]resource.Quantity)
		def[corev1.ResourceCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		def[corev1.ResourceMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		defReq := make(map[v1.ResourceName]resource.Quantity)
		defReq[corev1.ResourceCPU], err = resource.ParseQuantity(cpuRequest)
		require.NoError(t, err)
		defReq[corev1.ResourceMemory], err = resource.ParseQuantity(memoryRequest)
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

func networkPolicySameNamespace() namespaceObjectsCheck {
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

func networkPolicyAllowFromIngress() namespaceObjectsCheck {
	return networkPolicyIngress("allow-from-openshift-ingress", "ingress")
}

func networkPolicyAllowFromMonitoring() namespaceObjectsCheck {
	return networkPolicyIngress("allow-from-openshift-monitoring", "monitoring")
}

func networkPolicyIngress(name, group string) namespaceObjectsCheck {
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

func clusterResourceQuota(cpuLimit, cpuRequest, memoryLimit string) clusterObjectsCheck {
	return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName string) {
		quota, err := memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s", userName))
		require.NoError(t, err)

		hard := make(map[v1.ResourceName]resource.Quantity)
		hard[corev1.ResourceLimitsCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		hard[corev1.ResourceLimitsMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		hard[corev1.ResourceLimitsEphemeralStorage], err = resource.ParseQuantity("7Gi")
		require.NoError(t, err)
		hard[corev1.ResourceRequestsCPU], err = resource.ParseQuantity(cpuRequest)
		require.NoError(t, err)
		hard[corev1.ResourceRequestsMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		hard[corev1.ResourceRequestsStorage], err = resource.ParseQuantity("7Gi")
		require.NoError(t, err)
		hard[corev1.ResourceRequestsEphemeralStorage], err = resource.ParseQuantity("7Gi")
		require.NoError(t, err)
		hard[corev1.ResourcePersistentVolumeClaims], err = resource.ParseQuantity("5")
		require.NoError(t, err)
		hard[corev1.ResourcePods], err = resource.ParseQuantity("100")
		require.NoError(t, err)
		hard[corev1.ResourceReplicationControllers], err = resource.ParseQuantity("100")
		require.NoError(t, err)
		hard[corev1.ResourceServices], err = resource.ParseQuantity("100")
		require.NoError(t, err)
		hard[corev1.ResourceSecrets], err = resource.ParseQuantity("100")
		require.NoError(t, err)
		hard[corev1.ResourceConfigMaps], err = resource.ParseQuantity("100")
		require.NoError(t, err)

		expetedQuotaSpec := quotav1.ClusterResourceQuotaSpec{
			Selector: quotav1.ClusterResourceQuotaSelector{
				AnnotationSelector: map[string]string{
					"openshift.io/requester": userName,
				},
			},
			Quota: v1.ResourceQuotaSpec{
				Hard: hard,
			},
		}
		assert.Equal(t, expetedQuotaSpec, quota.Spec)
	}
}

func numberOfToolchainRoles(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		roles := &rbacv1.RoleList{}
		err := memberAwait.Client.List(context.TODO(), roles, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, roles.Items, number)
	}
}

func numberOfToolchainRoleBindings(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		roleBindings := &rbacv1.RoleBindingList{}
		err := memberAwait.Client.List(context.TODO(), roleBindings, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, roleBindings.Items, number)
	}
}

func numberOfLimitRanges(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		limitRanges := &v1.LimitRangeList{}
		err := memberAwait.Client.List(context.TODO(), limitRanges, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, limitRanges.Items, number)
	}
}

func numberOfNetworkPolicies(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		nps := &netv1.NetworkPolicyList{}
		err := memberAwait.Client.List(context.TODO(), nps, providerMatchingLabels, client.InNamespace(ns.Name))
		require.NoError(t, err)
		assert.Len(t, nps.Items, number)
	}
}

func numberOfClusterResourceQuotas(number int) clusterObjectsCheck {
	return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName string) {
		quotas := &quotav1.ClusterResourceQuotaList{}
		matchingLabels := client.MatchingLabels(map[string]string{ // make sure we only list the ClusterResourceQuota resources associated with the given "userName"
			"toolchain.dev.openshift.com/provider": "codeready-toolchain",
			"toolchain.dev.openshift.com/owner":    userName,
		})
		err := memberAwait.Client.List(context.TODO(), quotas, matchingLabels)
		require.NoError(t, err)
		assert.Len(t, quotas.Items, number)
	}
}
