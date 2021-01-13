package tiers

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"

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

const (
	// tier names
	basic                     = "basic"
	basicdeactivationdisabled = "basicdeactivationdisabled"
	advanced                  = "advanced"
	team                      = "team"

	// common CPU limits
	defaultCpuLimit = "500m"
	cpuLimit        = "10000m"
)

var (
	providerMatchingLabels = client.MatchingLabels(map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"})
	commonChecks           = []namespaceObjectsCheck{
		userEditRoleBinding(),
		numberOfLimitRanges(1),
	}
)

func NewChecks(tier string) (TierChecks, error) {
	switch tier {
	case basic:
		return &basicTierChecks{tierName: basic}, nil

	case basicdeactivationdisabled:
		// we want the basicdeactivationdisabled tier to have the same resources as the basic tier with the only difference being auto deactivation disabled
		return &basicdeactivationdisabledTierChecks{basicTierChecks{tierName: basicdeactivationdisabled}}, nil

	case advanced:
		return &advancedTierChecks{tierName: advanced}, nil

	case team:
		return &teamTierChecks{tierName: team}, nil

	default:
		return nil, fmt.Errorf("no assertion implementation found for %s", tier)
	}
}

type TierChecks interface {
	GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck
	GetClusterObjectChecks() []clusterObjectsCheck
	GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs
	GetTierObjectChecks() []tierObjectCheck
}

type basicdeactivationdisabledTierChecks struct {
	basicTierChecks
}

func (a *basicdeactivationdisabledTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 0)}
}

type basicTierChecks struct {
	tierName string
}

func (a *basicTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 30)}
}

func (a *basicTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := append(commonChecks,
		a.limitRangeByType(nsType),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2))

	checks = append(checks, commonNetworkPolicyChecks()...)

	switch nsType {
	case "code":
		checks = append(checks, networkPolicyAllowFromCRW(), networkPolicyAllowFromOtherNamespace("dev", "stage"), numberOfNetworkPolicies(6))
	case "dev":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("code", "stage"), numberOfNetworkPolicies(5))
	case "stage":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("code", "dev"), numberOfNetworkPolicies(5))
	}
	return checks
}

func (a *basicTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "code", "dev", "stage")
	return templateRefs
}

func (a *basicTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota(cpuLimit, "1750m", "7Gi"),
		numberOfClusterResourceQuotas(1),
		idlers("code", "dev", "stage"),
	}
}

func (a *basicTierChecks) limitRangeByType(nsType string) namespaceObjectsCheck {
	switch nsType {
	case "code":
		return limitRange("1", "512Mi", "60m", "307Mi")
	case "dev":
		return limitRange(defaultCpuLimit, "750Mi", "10m", "64Mi")
	default:
		return limitRange(defaultCpuLimit, "512Mi", "10m", "64Mi")
	}
}

func commonNetworkPolicyChecks() []namespaceObjectsCheck {
	return []namespaceObjectsCheck{
		networkPolicySameNamespace(),
		networkPolicyAllowFromMonitoring(),
		networkPolicyAllowFromIngress(),
	}
}

type advancedTierChecks struct {
	tierName string
}

func (a *advancedTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 0)}
}

func (a *advancedTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := append(commonChecks,
		a.limitRangeByType(nsType),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2))

	checks = append(checks, commonNetworkPolicyChecks()...)

	switch nsType {
	case "code":
		checks = append(checks, networkPolicyAllowFromCRW(), networkPolicyAllowFromOtherNamespace("dev", "stage"), numberOfNetworkPolicies(6))
	case "dev":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("code", "stage"), numberOfNetworkPolicies(5))
	case "stage":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("code", "dev"), numberOfNetworkPolicies(5))
	}
	return checks
}

func (a *advancedTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota(cpuLimit, "1750m", "7Gi"),
		numberOfClusterResourceQuotas(1),
	}
}

func (a *advancedTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "code", "dev", "stage")
	return templateRefs
}

func (a *advancedTierChecks) limitRangeByType(nsType string) namespaceObjectsCheck {
	switch nsType {
	case "code":
		return limitRange("1", "512Mi", "60m", "307Mi")
	case "dev":
		return limitRange(defaultCpuLimit, "750Mi", "10m", "64Mi")
	default:
		return limitRange(defaultCpuLimit, "512Mi", "10m", "64Mi")
	}
}

type teamTierChecks struct {
	tierName string
}

func (a *teamTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 0)}
}

func (a *teamTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := append(commonChecks,
		limitRange(defaultCpuLimit, "1Gi", "10m", "64Mi"),
		rbacEditRoleBinding(),
		rbacEditRole(),
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2),
	)

	checks = append(checks, commonNetworkPolicyChecks()...)

	switch nsType {
	case "dev":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("stage"), numberOfNetworkPolicies(4))
	case "stage":
		checks = append(checks, networkPolicyAllowFromOtherNamespace("dev"), numberOfNetworkPolicies(4))
	}
	return checks
}

func (a *teamTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "dev", "stage")
	return templateRefs
}

func (a *teamTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{
		clusterResourceQuota(cpuLimit, "2000m", "15Gi"),
		numberOfClusterResourceQuotas(1),
		idlers("dev", "stage"),
	}
}

// verifyNsTypes checks that there's a namespace.TemplateRef that begins with `<tier>-<type>` for each given templateRef (and no more, no less)
func verifyNsTypes(t *testing.T, tier string, templateRefs TemplateRefs, expectedNSTypes ...string) {
	require.Len(t, templateRefs.Namespaces, len(expectedNSTypes))
	actualNSTypes := make([]string, len(expectedNSTypes))
	for i, templateRef := range templateRefs.Namespaces {
		actualTier, actualType, _, err := wait.Split(templateRef)
		require.NoError(t, err)
		require.Equal(t, tier, actualTier)
		actualNSTypes[i] = actualType
	}
	// now compare with the given `nsTypes`
	assert.ElementsMatch(t, expectedNSTypes, actualNSTypes)
}

type namespaceObjectsCheck func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string)

type clusterObjectsCheck func(t *testing.T, memberAwait *wait.MemberAwaitility, userName string)

type tierObjectCheck func(t *testing.T, hostAwait *wait.HostAwaitility)

func nsTemplateTier(tierName string, days int) tierObjectCheck {
	return func(t *testing.T, hostAwait *wait.HostAwaitility) {
		tier, err := hostAwait.WaitForNSTemplateTier(tierName)
		require.NoError(t, err)
		require.Equal(t, days, tier.Spec.DeactivationTimeoutDays)
	}
}

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

func networkPolicyAllowFromOtherNamespace(otherNamespaceKinds ...string) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		var networkPolicyPeers []netv1.NetworkPolicyPeer
		for _, other := range otherNamespaceKinds {
			networkPolicyPeers = append(networkPolicyPeers, netv1.NetworkPolicyPeer{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"name": fmt.Sprintf("%s-%s", userName, other),
					},
				},
			})
		}

		np, err := memberAwait.WaitForNetworkPolicy(ns, "allow-from-other-user-namespaces")
		require.NoError(t, err)
		expected := &netv1.NetworkPolicy{
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress: []netv1.NetworkPolicyIngressRule{
					{
						From: networkPolicyPeers,
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

func networkPolicyAllowFromCRW() namespaceObjectsCheck {
	return networkPolicyIngress("allow-from-codeready-workspaces-operator", "codeready-workspaces")
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

func idlers(namespaceTypes ...string) clusterObjectsCheck {
	return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName string) {
		for _, nt := range namespaceTypes {
			idler, err := memberAwait.WaitForIdler(fmt.Sprintf("%s-%s", userName, nt))
			require.NoError(t, err)
			assert.Equal(t, userName, idler.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
			assert.Equal(t, int32(28800), idler.Spec.TimeoutSeconds)
		}

		// Make sure there is no unexpected idlers
		idlers := &v1alpha1.IdlerList{}
		err := memberAwait.Client.List(context.TODO(), idlers,
			client.MatchingLabels(map[string]string{
				"toolchain.dev.openshift.com/provider": "codeready-toolchain",
				"toolchain.dev.openshift.com/owner":    userName,
			}))
		require.NoError(t, err)
		assert.Len(t, idlers.Items, len(namespaceTypes))
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
		hard[corev1.ResourceRequestsStorage], err = resource.ParseQuantity("15Gi")
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
