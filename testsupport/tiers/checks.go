package tiers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

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
	advanced                 = "advanced"
	appstudio                = "appstudio"
	base                     = "base"
	base1ns                  = "base1ns"
	basedeactivationdisabled = "basedeactivationdisabled"
	baseextended             = "baseextended"
	baseextendedidling       = "baseextendedidling"
	baselarge                = "baselarge"
	hackathon                = "hackathon"
	testtier                 = "test"

	// common CPU limits
	defaultCPULimit = "1"
	cpuLimit        = "20000m" // All tiers
)

var (
	providerMatchingLabels = client.MatchingLabels(map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"})
	commonChecks           = []namespaceObjectsCheck{
		userEditRoleBinding(),
		numberOfLimitRanges(1),
	}
)

type TierChecks interface {
	GetClusterObjectChecks() []clusterObjectsCheck
	GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs
	GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck
	GetTierObjectChecks() []tierObjectCheck
}

func NewChecksForTier(tier *toolchainv1alpha1.NSTemplateTier) (TierChecks, error) {
	switch tier.Name {
	case base:
		return &baseTierChecks{tierName: base}, nil

	case base1ns:
		return &base1nsTierChecks{baseTierChecks{tierName: base1ns}}, nil

	case baselarge:
		return &baselargeTierChecks{baseTierChecks{tierName: baselarge}}, nil

	case baseextended:
		return &baseextendedTierChecks{baseTierChecks{tierName: baseextended}}, nil

	case baseextendedidling:
		return &baseextendedidlingTierChecks{baseTierChecks{tierName: baseextendedidling}}, nil

	case basedeactivationdisabled:
		return &basedeactivationdisabledTierChecks{baseTierChecks{tierName: basedeactivationdisabled}}, nil

	case hackathon:
		return &hackathonTierChecks{baseTierChecks{tierName: hackathon}}, nil

	case advanced:
		return &advancedTierChecks{baseTierChecks{tierName: advanced}}, nil

	case appstudio:
		return &appstudioTierChecks{tierName: appstudio}, nil

	case testtier:
		return &testTierChecks{tierName: testtier}, nil

	default:
		return nil, fmt.Errorf("no assertion implementation found for %s", tier.Name)
	}
}

// NewChecksForCustomTier returns a `TierChecks` initialized with the tiers used in the CustomNSTemplateTier
func NewChecksForCustomTier(tier *CustomNSTemplateTier) (TierChecks, error) {
	c := &customTierChecks{}

	// using the checks of the "cluster resources" NSTemplateTier
	clusterChecks, err := NewChecksForTier(tier.ClusterResourcesTier)
	if err != nil {
		return nil, err
	}
	c.getClusterObjectChecks = clusterChecks.GetClusterObjectChecks

	// using the checks of the "namespace resources" NSTemplateTier
	namespaceChecks, err := NewChecksForTier(tier.ClusterResourcesTier)
	if err != nil {
		return nil, err
	}
	c.getNamespaceObjectChecks = namespaceChecks.GetNamespaceObjectChecks

	// using default/wrapped tier for the deactivation timeout
	c.getTierObjectChecks = func() []tierObjectCheck {
		return []tierObjectCheck{
			nsTemplateTier(tier.Name, tier.Spec.DeactivationTimeoutDays),
		}
	}

	var clusterResourcesTmplRef *string
	if tier.NSTemplateTier.Spec.ClusterResources != nil {
		clusterResourcesTmplRef = &tier.NSTemplateTier.Spec.ClusterResources.TemplateRef
	}
	namespaceTmplRefs := make([]string, len(tier.NSTemplateTier.Spec.Namespaces))
	for i, ns := range tier.NSTemplateTier.Spec.Namespaces {
		namespaceTmplRefs[i] = ns.TemplateRef
	}

	c.getExpectedTemplateRefs = func(_ *wait.HostAwaitility) TemplateRefs {
		return TemplateRefs{
			ClusterResources: clusterResourcesTmplRef,
			Namespaces:       namespaceTmplRefs,
		}
	}
	return c, nil
}

var _ TierChecks = &customTierChecks{}

type customTierChecks struct {
	getClusterObjectChecks   func() []clusterObjectsCheck
	getExpectedTemplateRefs  func(hostAwait *wait.HostAwaitility) TemplateRefs
	getNamespaceObjectChecks func(nsType string) []namespaceObjectsCheck
	getTierObjectChecks      func() []tierObjectCheck
}

func (c *customTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	return c.getNamespaceObjectChecks(nsType)
}

func (c *customTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return c.getClusterObjectChecks()
}

func (c *customTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	return c.getExpectedTemplateRefs(hostAwait)
}

func (c *customTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return c.getTierObjectChecks()
}

type baseTierChecks struct {
	tierName string
}

func (a *baseTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 30)}
}

func (a *baseTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := append(commonChecks,
		limitRange(defaultCPULimit, "750Mi", "10m", "64Mi"),
		rbacEditRoleBinding(),
		rbacEditRole(),
		crtadminPodsRoleBinding(),
		crtadminViewRoleBinding(),
		execPodsRole(),
		numberOfToolchainRoles(2),
		numberOfToolchainRoleBindings(4))

	checks = append(checks, commonNetworkPolicyChecks()...)

	var otherNamespaceKind string
	switch nsType {
	case "dev":
		otherNamespaceKind = "stage"
	case "stage":
		otherNamespaceKind = "dev"
	}
	checks = append(checks, networkPolicyAllowFromCRW(), networkPolicyAllowFromOtherNamespace(otherNamespaceKind), numberOfNetworkPolicies(7))

	return checks
}

func (a *baseTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "dev", "stage")
	return templateRefs
}

func (a *baseTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(43200, "dev", "stage"))
}

type base1nsTierChecks struct {
	baseTierChecks
}

func (a *base1nsTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := append(commonChecks,
		limitRange(defaultCPULimit, "750Mi", "10m", "64Mi"),
		rbacEditRoleBinding(),
		rbacEditRole(),
		crtadminPodsRoleBinding(),
		crtadminViewRoleBinding(),
		execPodsRole(),
		numberOfToolchainRoles(2),
		numberOfToolchainRoleBindings(4))

	checks = append(checks, commonNetworkPolicyChecks()...)
	checks = append(checks, networkPolicyAllowFromCRW(), numberOfNetworkPolicies(6))

	return checks
}

func (a *base1nsTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "dev")
	return templateRefs
}

func (a *base1nsTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(43200, "dev"))
}

type baselargeTierChecks struct {
	baseTierChecks
}

func (a *baselargeTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "16Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(43200, "dev", "stage"))
}

func (a *baselargeTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 90)}
}

type baseextendedTierChecks struct {
	baseTierChecks
}

func (a *baseextendedTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 180)}
}

type baseextendedidlingTierChecks struct {
	baseTierChecks
}

func (a *baseextendedidlingTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(518400, "dev", "stage"))
}

type basedeactivationdisabledTierChecks struct {
	baseTierChecks
}

func (a *basedeactivationdisabledTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 0)}
}

func commonNetworkPolicyChecks() []namespaceObjectsCheck {
	return []namespaceObjectsCheck{
		networkPolicySameNamespace(),
		networkPolicyAllowFromMonitoring(),
		networkPolicyAllowFromIngress(),
		networkPolicyAllowFromOlmNamespaces(),
		networkPolicyAllowFromConsoleNamespaces(),
	}
}

type advancedTierChecks struct {
	baseTierChecks
}

func (a *advancedTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 0)}
}

func (a *advancedTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "16Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(0, "dev", "stage"))
}

func (a *advancedTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "dev", "stage")
	return templateRefs
}

type hackathonTierChecks struct {
	baseTierChecks
}

func (a *hackathonTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 80)}
}

// testTierChecks checks only that the "test" tier exists and has correct template references.
// It does not check the test tier resources
type testTierChecks struct {
	tierName string
}

func (a *testTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{}
}

func (a *testTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	return []namespaceObjectsCheck{}
}

func (a *testTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "dev", "stage")
	return templateRefs
}

func (a *testTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{}
}

type appstudioTierChecks struct {
	tierName string
}

func (a *appstudioTierChecks) GetTierObjectChecks() []tierObjectCheck {
	return []tierObjectCheck{nsTemplateTier(a.tierName, 30)}
}

func (a *appstudioTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := []namespaceObjectsCheck{
		limitRange(defaultCPULimit, "750Mi", "10m", "64Mi"),
		appstudioServiceAccount(),
		appstudioUserActionsRole(),
		appstudioUserActionsRoleBinding(),
		appstudioViewRoleBinding(),
		toolchainSaReadRole(),
		userSaReadRoleBinding(),
		numberOfLimitRanges(1),
		numberOfToolchainRoles(2),
		numberOfToolchainRoleBindings(3),
		numberOfToolchainServiceAccounts(1),
		gitOpsServiceLabel(),
	}

	checks = append(checks, append(commonNetworkPolicyChecks(), networkPolicyAllowFromCRW(), numberOfNetworkPolicies(6))...)
	return checks
}

func (a *appstudioTierChecks) GetExpectedTemplateRefs(hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(hostAwait, a.tierName)
	verifyNsTypes(hostAwait.T, a.tierName, templateRefs, "appstudio")
	return templateRefs
}

func (a *appstudioTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "15Gi"),
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(),
		idlers(43200, ""))
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

type clusterObjectsCheck func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string)

type tierObjectCheck func(t *testing.T, hostAwait *wait.HostAwaitility)

func nsTemplateTier(tierName string, deactivationDays int) tierObjectCheck {
	return func(t *testing.T, hostAwait *wait.HostAwaitility) {
		tier, err := hostAwait.WaitForNSTemplateTier(tierName)
		require.NoError(t, err)
		require.Equal(t, deactivationDays, tier.Spec.DeactivationTimeoutDays)
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
		assert.Equal(t, userName, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
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
		assert.Equal(t, userName, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func crtadminViewRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, "crtadmin-view")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "crtadmin-users-view", rb.Subjects[0].Name)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, userName, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func crtadminPodsRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, "crtadmin-pods")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "crtadmin-users-view", rb.Subjects[0].Name)
		assert.Equal(t, "exec-pods", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, userName, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func execPodsRole() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(ns, "exec-pods")
		require.NoError(t, err)
		assert.Len(t, role.Rules, 1)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods/exec"},
					Verbs:     []string{"get", "list", "watch", "create", "delete", "update"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, userName, role.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
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
		assert.Equal(t, userName, role.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func limitRange(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) namespaceObjectsCheck { // nolint:unparam
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
	return networkPolicyIngressFromPolicyGroup("allow-from-openshift-ingress", "ingress")
}

func networkPolicyAllowFromMonitoring() namespaceObjectsCheck {
	return networkPolicyIngressFromPolicyGroup("allow-from-openshift-monitoring", "monitoring")
}

func networkPolicyAllowFromOlmNamespaces() namespaceObjectsCheck {
	return networkPolicyIngress("allow-from-olm-namespaces", "openshift.io/scc", "anyuid")
}

func networkPolicyAllowFromConsoleNamespaces() namespaceObjectsCheck {
	return networkPolicyIngressFromPolicyGroup("allow-from-console-namespaces", "console")
}

func networkPolicyAllowFromCRW() namespaceObjectsCheck {
	return networkPolicyIngressFromPolicyGroup("allow-from-codeready-workspaces-operator", "codeready-workspaces")
}

func networkPolicyIngressFromPolicyGroup(name, group string) namespaceObjectsCheck {
	return networkPolicyIngress(name, "network.openshift.io/policy-group", group)
}

func networkPolicyIngress(name, labelName, labelValue string) namespaceObjectsCheck {
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
								NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{labelName: labelValue}},
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

type clusterObjectsCheckCreator func() clusterObjectsCheck

func clusterObjectsChecks(checkCreator ...clusterObjectsCheckCreator) []clusterObjectsCheck {
	var checks []clusterObjectsCheck
	for _, createCheck := range checkCreator {
		checks = append(checks, createCheck())
	}
	return checks
}

func idlers(timeoutSeconds int, namespaceTypes ...string) clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			for _, nt := range namespaceTypes {
				var idlerName string
				if nt == "" {
					idlerName = userName
				} else {
					idlerName = fmt.Sprintf("%s-%s", userName, nt)
				}
				idler, err := memberAwait.WaitForIdler(idlerName, wait.IdlerHasTier(tierLabel), wait.IdlerHasTimeoutSeconds(timeoutSeconds))
				require.NoError(t, err)
				assert.Equal(t, userName, idler.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
			}

			// Make sure there is no unexpected idlers
			idlers := &toolchainv1alpha1.IdlerList{}
			err := memberAwait.Client.List(context.TODO(), idlers,
				client.MatchingLabels(map[string]string{
					"toolchain.dev.openshift.com/provider": "codeready-toolchain",
					"toolchain.dev.openshift.com/owner":    userName,
				}))
			require.NoError(t, err)
			assert.Len(t, idlers.Items, len(namespaceTypes))
		}
	}
}

func clusterResourceQuotaCompute(cpuLimit, cpuRequest, memoryLimit, storageLimit string) clusterObjectsCheckCreator { // nolint:unparam
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
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
			hard[corev1.ResourceRequestsStorage], err = resource.ParseQuantity(storageLimit)
			require.NoError(t, err)
			hard[corev1.ResourceRequestsEphemeralStorage], err = resource.ParseQuantity("7Gi")
			require.NoError(t, err)
			hard[count(corev1.ResourcePersistentVolumeClaims)], err = resource.ParseQuantity("5")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-compute", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaDeployments() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count("deployments.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("deploymentconfigs.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count(corev1.ResourcePods)], err = resource.ParseQuantity("50")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-deployments", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaReplicas() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count("replicasets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count(corev1.ResourceReplicationControllers)], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-replicas", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaRoutes() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count("routes.route.openshift.io")], err = resource.ParseQuantity("10")
			require.NoError(t, err)
			hard[count("ingresses.extensions")], err = resource.ParseQuantity("10")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-routes", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaJobs() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count("daemonsets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("statefulsets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("jobs.batch")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("cronjobs.batch")], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-jobs", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaServices() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceServices)], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-services", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaBuildConfig() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count("buildconfigs.build.openshift.io")], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-bc", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaSecrets() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceSecrets)], err = resource.ParseQuantity("100")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-secrets", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaConfigMap() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[v1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceConfigMaps)], err = resource.ParseQuantity("100")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(fmt.Sprintf("for-%s-cm", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaMatches(userName, tierName string, hard map[v1.ResourceName]resource.Quantity) wait.ClusterResourceQuotaWaitCriterion {
	return wait.ClusterResourceQuotaWaitCriterion{
		Match: func(actual *quotav1.ClusterResourceQuota) bool {
			expectedQuotaSpec := quotav1.ClusterResourceQuotaSpec{
				Selector: quotav1.ClusterResourceQuotaSelector{
					AnnotationSelector: map[string]string{
						"openshift.io/requester": userName,
					},
				},
				Quota: v1.ResourceQuotaSpec{
					Hard: hard,
				},
			}
			return actual.Labels != nil && tierName == actual.Labels["toolchain.dev.openshift.com/tier"] &&
				reflect.DeepEqual(expectedQuotaSpec, actual.Spec)
		},
		Diff: func(actual *quotav1.ClusterResourceQuota) string {
			return fmt.Sprintf("expected ClusterResourceQuota to match for %s/%s: %s", userName, tierName, wait.Diff(hard, actual.Spec.Quota.Hard))
		},
	}
}

func count(resource v1.ResourceName) v1.ResourceName {
	return v1.ResourceName(fmt.Sprintf("count/%s", resource))
}

func numberOfToolchainRoles(number int) namespaceObjectsCheck { // nolint:unparam
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		err := memberAwait.WaitForExpectedNumberOfResources("Roles", number, func() (int, error) {
			roles := &rbacv1.RoleList{}
			err := memberAwait.Client.List(context.TODO(), roles, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(roles.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfToolchainRoleBindings(number int) namespaceObjectsCheck { // nolint:unparam
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		err := memberAwait.WaitForExpectedNumberOfResources("RoleBindings", number, func() (int, error) {
			roleBindings := &rbacv1.RoleBindingList{}
			err := memberAwait.Client.List(context.TODO(), roleBindings, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(roleBindings.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfToolchainServiceAccounts(number int) namespaceObjectsCheck { // nolint:unparam
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		err := memberAwait.WaitForExpectedNumberOfResources("ServiceAccounts", number, func() (int, error) {
			serviceAccounts := &corev1.ServiceAccountList{}
			err := memberAwait.Client.List(context.TODO(), serviceAccounts, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(serviceAccounts.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfLimitRanges(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		err := memberAwait.WaitForExpectedNumberOfResources("LimitRanges", number, func() (int, error) {
			limitRanges := &v1.LimitRangeList{}
			err := memberAwait.Client.List(context.TODO(), limitRanges, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(limitRanges.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfNetworkPolicies(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		err := memberAwait.WaitForExpectedNumberOfResources("NetworkPolicies", number, func() (int, error) {
			nps := &netv1.NetworkPolicyList{}
			err := memberAwait.Client.List(context.TODO(), nps, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(nps.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfClusterResourceQuotas() clusterObjectsCheckCreator {
	expectedCRQs := 9
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			err := memberAwait.WaitForExpectedNumberOfResources("ClusterResourceQuotas", expectedCRQs, func() (int, error) {
				quotas := &quotav1.ClusterResourceQuotaList{}
				matchingLabels := client.MatchingLabels(map[string]string{ // make sure we only list the ClusterResourceQuota resources associated with the given "userName"
					"toolchain.dev.openshift.com/provider": "codeready-toolchain",
					"toolchain.dev.openshift.com/owner":    userName,
				})
				err := memberAwait.Client.List(context.TODO(), quotas, matchingLabels)
				require.NoError(t, err)
				return len(quotas.Items), err
			})
			require.NoError(t, err)
		}
	}
}

// Appstudio tier specific objects

func gitOpsServiceLabel() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		// TODO fix for migration/existing namespaces cases
		labelWaitCriterion := []wait.LabelWaitCriterion{}
		if !strings.HasPrefix(ns.Name, "migration-") {
			labelWaitCriterion = append(labelWaitCriterion, wait.UntilObjectHasLabel("argocd.argoproj.io/managed-by", "gitops-service-argocd"))
		}
		_, err := memberAwait.WaitForNamespaceWithName(ns.Name, labelWaitCriterion...)
		require.NoError(t, err)
	}
}

func appstudioServiceAccount() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		_, err := memberAwait.WaitForServiceAccount(ns, fmt.Sprintf("appstudio-%s", userName))
		require.NoError(t, err)
	}
}

func appstudioUserActionsRole() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(ns, "appstudio-user-actions")
		require.NoError(t, err)
		assert.Len(t, role.Rules, 7)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"managed-gitops.redhat.com"},
					Resources: []string{"gitopsdeployments"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"applications", "components", "componentdetectionqueries"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokenbindings"},
					Verbs:     []string{"create", "get", "list", "watch", "delete"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokens"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"tekton.dev"},
					Resources: []string{"pipelineruns"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"secrets"},
					Verbs:     []string{"create", "delete"},
				},
				{
					APIGroups: []string{"results.tekton.dev"},
					Resources: []string{"results", "records"},
					Verbs:     []string{"get", "list"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func appstudioUserActionsRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-user-actions", userName))
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, "appstudio-"+userName, rb.Subjects[0].Name)
		assert.Equal(t, "", rb.Subjects[0].APIGroup)
		assert.Equal(t, "appstudio-user-actions", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func appstudioViewRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", userName))
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, "appstudio-"+userName, rb.Subjects[0].Name)
		assert.Equal(t, "", rb.Subjects[0].APIGroup)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func userSaReadRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(ns, "sa-read")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "system:serviceaccounts:"+memberAwait.Namespace, rb.Subjects[0].Name)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.Subjects[0].APIGroup)
		assert.Equal(t, "toolchain-sa-read", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}

func toolchainSaReadRole() namespaceObjectsCheck {
	return func(t *testing.T, ns *v1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(ns, "toolchain-sa-read")
		require.NoError(t, err)
		assert.Len(t, role.Rules, 1)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"secrets", "serviceaccounts"},
					Verbs:     []string{"get", "list"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}
