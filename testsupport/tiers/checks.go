package tiers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/davecgh/go-spew/spew"
	quotav1 "github.com/openshift/api/quota/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// tier names
	advanced           = "advanced"
	appstudio          = "appstudio"
	base               = "base"
	base1ns            = "base1ns"
	base1ns6didler     = "base1ns6didler"
	base1nsnoidling    = "base1nsnoidling"
	baseextendedidling = "baseextendedidling"
	baselarge          = "baselarge"
	testTier           = "test"

	// common CPU limits
	defaultCPULimit = "1"
	cpuLimit        = "20000m" // All tiers
)

var (
	providerMatchingLabels = client.MatchingLabels(map[string]string{"toolchain.dev.openshift.com/provider": "codeready-toolchain"})
)

type TierChecks interface {
	GetClusterObjectChecks() []clusterObjectsCheck
	GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs
	GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck
	GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error)
}

func NewChecksForTier(tier *toolchainv1alpha1.NSTemplateTier) (TierChecks, error) {
	switch tier.Name {
	case base:
		return &baseTierChecks{tierName: base}, nil

	case base1ns:
		return &base1nsTierChecks{tierName: base1ns}, nil

	case base1nsnoidling:
		return &base1nsnoidlingTierChecks{base1nsTierChecks{tierName: base1nsnoidling}}, nil

	case base1ns6didler:
		return &base1ns6didlerTierChecks{base1nsTierChecks{tierName: base1ns6didler}}, nil

	case baselarge:
		return &baselargeTierChecks{baseTierChecks{tierName: baselarge}}, nil

	case baseextendedidling:
		return &baseextendedidlingTierChecks{baseTierChecks{tierName: baseextendedidling}}, nil

	case advanced:
		return &advancedTierChecks{baseTierChecks{tierName: advanced}}, nil

	case appstudio:
		return &appstudioTierChecks{tierName: appstudio}, nil

	case testTier:
		return &testTierChecks{tierName: testTier}, nil

	default:
		return nil, fmt.Errorf("no assertion implementation found for %s", tier.Name)
	}
}

// NewChecksForCustomTier returns a `TierChecks` initialized with the tiers used in the CustomNSTemplateTier
func NewChecksForCustomTier(t *testing.T, tier *CustomNSTemplateTier) TierChecks {
	c := &customTierChecks{
		t:    t,
		tier: tier,
	}
	return c
}

var _ TierChecks = &customTierChecks{}

type customTierChecks struct {
	t    *testing.T
	tier *CustomNSTemplateTier
}

func (c *customTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks, err := NewChecksForTier(c.tier.NamespaceResourcesTier)
	require.NoError(c.t, err)
	return checks.GetNamespaceObjectChecks(nsType)
}

func (c *customTierChecks) GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error) {
	checks, err := NewChecksForTier(c.tier.SpaceRolesTier)
	require.NoError(c.t, err)
	return checks.GetSpaceRoleChecks(spaceRoles)
}

func (c *customTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	checks, err := NewChecksForTier(c.tier.ClusterResourcesTier)
	require.NoError(c.t, err)
	return checks.GetClusterObjectChecks()
}

func (c *customTierChecks) GetExpectedTemplateRefs(_ *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	var clusterResourcesTmplRef *string
	if c.tier.NSTemplateTier.Spec.ClusterResources != nil {
		clusterResourcesTmplRef = &c.tier.NSTemplateTier.Spec.ClusterResources.TemplateRef
	}
	namespaceTmplRefs := make([]string, len(c.tier.NSTemplateTier.Spec.Namespaces))
	for i, ns := range c.tier.NSTemplateTier.Spec.Namespaces {
		namespaceTmplRefs[i] = ns.TemplateRef
	}

	return TemplateRefs{
		ClusterResources: clusterResourcesTmplRef,
		Namespaces:       namespaceTmplRefs,
	}
}

type baseTierChecks struct {
	tierName string
}

func (a *baseTierChecks) GetNamespaceObjectChecks(nsType string) []namespaceObjectsCheck {
	checks := []namespaceObjectsCheck{
		numberOfLimitRanges(1),
		limitRange(defaultCPULimit, "750Mi", "10m", "64Mi"),
		execPodsRole(),
		crtadminPodsRoleBinding(),
		crtadminViewRoleBinding(),
	}
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

func (a *baseTierChecks) GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error) {
	checks := []spaceRoleObjectsCheck{}
	roles := 0
	rolebindings := 0
	for role, usernames := range spaceRoles {
		switch role {
		case "admin":
			checks = append(checks, rbacEditRole())
			roles++
			for _, userName := range usernames {
				checks = append(checks,
					rbacEditRoleBinding(userName),
					userEditRoleBinding(userName),
				)
				rolebindings += 2
			}
		default:
			return nil, fmt.Errorf("unexpected template name: '%s'", role)
		}
	}
	// also count the roles, rolebindings
	checks = append(checks,
		numberOfToolchainRoles(roles+1),               // +1 for `exec-pods`
		numberOfToolchainRoleBindings(rolebindings+2), // +2 for `crtadmin-pods` and `crtadmin-view`
	)
	return checks, nil
}

func (a *baseTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "dev", "stage")
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
		numberOfClusterResourceQuotas(9),
		idlers(43200, "dev", "stage"))
}

type base1nsTierChecks struct {
	tierName string
}

func (a *base1nsTierChecks) GetNamespaceObjectChecks(_ string) []namespaceObjectsCheck {
	checks := []namespaceObjectsCheck{
		resourceQuotaComputeDeploy("20", "14Gi", "1750m", "14Gi"),
		resourceQuotaComputeBuild("20", "10Gi", "2", "10Gi"),
		resourceQuotaStorage("15Gi", "15Gi", "15Gi", "5"),
		limitRange(defaultCPULimit, "1000Mi", "10m", "64Mi"),
		numberOfLimitRanges(1),
		execPodsRole(),
		crtadminPodsRoleBinding(),
		crtadminViewRoleBinding(),
	}
	checks = append(checks, commonNetworkPolicyChecks()...)
	checks = append(checks, networkPolicyAllowFromCRW(), numberOfNetworkPolicies(6))
	return checks
}

func (a *base1nsTierChecks) GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error) {
	checks := []spaceRoleObjectsCheck{}
	roles := 0
	rolebindings := 0
	for role, usernames := range spaceRoles {
		switch role {
		case "admin":
			checks = append(checks, rbacEditRole())
			roles++
			for _, userName := range usernames {
				checks = append(checks,
					rbacEditRoleBinding(userName),
					userEditRoleBinding(userName),
				)
				rolebindings += 2
			}
		default:
			return nil, fmt.Errorf("unexpected template name: '%s'", role)
		}
	}
	// also count the roles, rolebindings
	checks = append(checks,
		numberOfToolchainRoles(roles+1),               // +1 for `exec-pods`
		numberOfToolchainRoleBindings(rolebindings+2), // +2 for `crtadmin-pods` and `crtadmin-view`
	)
	return checks, nil
}

func (a *base1nsTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "dev")
	return templateRefs
}

func (a *base1nsTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(43200, "dev"))
}

type base1nsnoidlingTierChecks struct {
	base1nsTierChecks
}

func (a *base1nsnoidlingTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(0, "dev"))
}

type base1ns6didlerTierChecks struct {
	base1nsTierChecks
}

func (a *base1ns6didlerTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(518400, "dev"))
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
		numberOfClusterResourceQuotas(9),
		idlers(43200, "dev", "stage"))
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
		numberOfClusterResourceQuotas(9),
		idlers(518400, "dev", "stage"))
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
		numberOfClusterResourceQuotas(9),
		idlers(0, "dev", "stage"))
}

func (a *advancedTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "dev", "stage")
	return templateRefs
}

// testTierChecks checks only that the "test" tier exists and has correct template references.
// It does not check the test tier resources
type testTierChecks struct {
	tierName string
}

func (a *testTierChecks) GetNamespaceObjectChecks(_ string) []namespaceObjectsCheck {
	return []namespaceObjectsCheck{}
}

func (a *testTierChecks) GetSpaceRoleChecks(_ map[string][]string) ([]spaceRoleObjectsCheck, error) {
	return []spaceRoleObjectsCheck{}, nil
}

func (a *testTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "dev")
	return templateRefs
}

func (a *testTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return []clusterObjectsCheck{}
}

type appstudioTierChecks struct {
	tierName string
}

func (a *appstudioTierChecks) GetNamespaceObjectChecks(_ string) []namespaceObjectsCheck {
	checks := []namespaceObjectsCheck{
		resourceQuotaComputeDeploy("20", "32Gi", "1750m", "32Gi"),
		resourceQuotaComputeBuild("20", "64Gi", "2", "32Gi"),
		resourceQuotaStorage("15Gi", "15Gi", "15Gi", "5"),
		limitRange("2", "2Gi", "10m", "256Mi"),
		numberOfLimitRanges(1),
		toolchainSaReadRole(),
		memberOperatorSaReadRoleBinding(),
		gitOpsServiceLabel(),
	}

	checks = append(checks, append(commonNetworkPolicyChecks(), networkPolicyAllowFromCRW(), numberOfNetworkPolicies(6))...)
	return checks
}

func (a *appstudioTierChecks) GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error) {
	checks := []spaceRoleObjectsCheck{}
	roles := 0
	rolebindings := 0
	for role, usernames := range spaceRoles {
		switch role {
		case "admin":
			checks = append(checks, appstudioUserActionsRole())
			roles++
			for _, userName := range usernames {
				checks = append(checks,
					appstudioUserActionsRoleBinding(userName),
					appstudioViewRoleBinding(userName),
				)
				rolebindings += 2
			}
		case "viewer":
			for _, userName := range usernames {
				checks = append(checks,
					appstudioViewRoleBinding(userName),
				)
				rolebindings++
			}
		default:
			return nil, fmt.Errorf("unexpected template name: '%s'", role)
		}
	}
	// also count the roles, rolebindings and service accounts
	checks = append(checks,
		numberOfToolchainRoles(roles+1),               // +1 for `toolchain-sa-read`
		numberOfToolchainRoleBindings(rolebindings+1), // +1 for `member-operator-sa-read`
	)
	return checks, nil
}

func (a *appstudioTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "appstudio")
	return templateRefs
}

func (a *appstudioTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments(),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(0, ""))
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

type namespaceObjectsCheck func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string)

type spaceRoleObjectsCheck func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string)

type clusterObjectsCheck func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string)

func userEditRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, userName+"-edit")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "edit", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func rbacEditRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, userName+"-rbac-edit")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "rbac-edit", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func crtadminViewRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "crtadmin-view")
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "crtadmin-users-view", rb.Subjects[0].Name)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func crtadminPodsRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "crtadmin-pods")
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(t, ns, "exec-pods")
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

func rbacEditRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "rbac-edit")
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
		assert.Equal(t, owner, role.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func resourceQuotaComputeDeploy(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating},
			Hard:   make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard[corev1.ResourceLimitsCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceLimitsMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsCPU], err = resource.ParseQuantity(cpuRequest)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsMemory], err = resource.ParseQuantity(memoryRequest)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "compute-deploy", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "compute-deploy", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaComputeBuild(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating},
			Hard:   make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard[corev1.ResourceLimitsCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceLimitsMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsCPU], err = resource.ParseQuantity(cpuRequest)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsMemory], err = resource.ParseQuantity(memoryRequest)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "compute-build", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "compute-build", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaStorage(ephemeralLimit, storageRequest, ephemeralRequest, pvcs string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard[corev1.ResourceLimitsEphemeralStorage], err = resource.ParseQuantity(ephemeralLimit)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsStorage], err = resource.ParseQuantity(storageRequest)
		require.NoError(t, err)
		spec.Hard[corev1.ResourceRequestsEphemeralStorage], err = resource.ParseQuantity(ephemeralRequest)
		require.NoError(t, err)
		spec.Hard[count(corev1.ResourcePersistentVolumeClaims)], err = resource.ParseQuantity(pvcs)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "storage", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "storage", criteria)
		require.NoError(t, err)
	}
}

func limitRange(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) namespaceObjectsCheck { // nolint:unparam
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		lr, err := memberAwait.WaitForLimitRange(t, ns, "resource-limits")
		require.NoError(t, err)
		def := make(map[corev1.ResourceName]resource.Quantity)
		def[corev1.ResourceCPU], err = resource.ParseQuantity(cpuLimit)
		require.NoError(t, err)
		def[corev1.ResourceMemory], err = resource.ParseQuantity(memoryLimit)
		require.NoError(t, err)
		defReq := make(map[corev1.ResourceName]resource.Quantity)
		defReq[corev1.ResourceCPU], err = resource.ParseQuantity(cpuRequest)
		require.NoError(t, err)
		defReq[corev1.ResourceMemory], err = resource.ParseQuantity(memoryRequest)
		require.NoError(t, err)
		assert.Equal(t, "codeready-toolchain", lr.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		expected := &corev1.LimitRange{
			Spec: corev1.LimitRangeSpec{
				Limits: []corev1.LimitRangeItem{
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		np, err := memberAwait.WaitForNetworkPolicy(t, ns, "allow-same-namespace")
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
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

		np, err := memberAwait.WaitForNetworkPolicy(t, ns, "allow-from-other-user-namespaces")
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		np, err := memberAwait.WaitForNetworkPolicy(t, ns, name)
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
				idler, err := memberAwait.WaitForIdler(t, idlerName, wait.IdlerHasTier(tierLabel), wait.IdlerHasTimeoutSeconds(timeoutSeconds))
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
			hard := make(map[corev1.ResourceName]resource.Quantity)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-compute", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaDeployments() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("deployments.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("deploymentconfigs.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count(corev1.ResourcePods)], err = resource.ParseQuantity("50")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-deployments", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaReplicas() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("replicasets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count(corev1.ResourceReplicationControllers)], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-replicas", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaRoutes() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("routes.route.openshift.io")], err = resource.ParseQuantity("10")
			require.NoError(t, err)
			hard[count("ingresses.extensions")], err = resource.ParseQuantity("10")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-routes", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaJobs() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("daemonsets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("statefulsets.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("jobs.batch")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("cronjobs.batch")], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-jobs", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaServices() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceServices)], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-services", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaBuildConfig() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("buildconfigs.build.openshift.io")], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-bc", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaSecrets() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceSecrets)], err = resource.ParseQuantity("100")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-secrets", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaConfigMap() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count(corev1.ResourceConfigMaps)], err = resource.ParseQuantity("100")
			require.NoError(t, err)

			criteria := clusterResourceQuotaMatches(userName, tierLabel, hard)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-cm", userName), criteria)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaMatches(userName, tierName string, hard map[corev1.ResourceName]resource.Quantity) wait.ClusterResourceQuotaWaitCriterion {
	return wait.ClusterResourceQuotaWaitCriterion{
		Match: func(actual *quotav1.ClusterResourceQuota) bool {
			expectedQuotaSpec := quotav1.ClusterResourceQuotaSpec{
				Selector: quotav1.ClusterResourceQuotaSelector{
					AnnotationSelector: map[string]string{
						"openshift.io/requester": userName,
					},
				},
				Quota: corev1.ResourceQuotaSpec{
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

func resourceQuotaMatches(namespace, name string, spec corev1.ResourceQuotaSpec) wait.ResourceQuotaWaitCriterion {
	return wait.ResourceQuotaWaitCriterion{
		Match: func(actual *corev1.ResourceQuota) bool {
			expectedQuotaSpec := spec
			return reflect.DeepEqual(expectedQuotaSpec, actual.Spec)
		},
		Diff: func(actual *corev1.ResourceQuota) string {
			return fmt.Sprintf("expected ResourceQuota to match (namespace=%s, name=%s):\n%s", namespace, name, wait.Diff(spec, actual.Spec))
		},
	}
}

func count(resource corev1.ResourceName) corev1.ResourceName {
	return corev1.ResourceName(fmt.Sprintf("count/%s", resource))
}

func numberOfToolchainRoles(number int) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		roles := &rbacv1.RoleList{}
		err := memberAwait.WaitForExpectedNumberOfResources(t, ns.Name, "Roles", number, func() (int, error) {
			err := memberAwait.Client.List(context.TODO(), roles, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(roles.Items), err
		})
		if err != nil {
			rs := make([]string, len(roles.Items))
			for i, r := range roles.Items {
				rs[i] = r.Name
			}
			t.Logf("found %d roles: %s", len(roles.Items), spew.Sdump(rs))
			if nsTmplSet, err := memberAwait.WaitForNSTmplSet(t, owner); err == nil {
				t.Logf("associated NSTemplateSet: %s", spew.Sdump(nsTmplSet))
			}
		}
		require.NoError(t, err)
	}
}

func numberOfToolchainRoleBindings(number int) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		roleBindings := &rbacv1.RoleBindingList{}
		err := memberAwait.WaitForExpectedNumberOfResources(t, ns.Name, "RoleBindings", number, func() (int, error) {
			err := memberAwait.Client.List(context.TODO(), roleBindings, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(roleBindings.Items), err
		})
		if err != nil {
			rbs := make([]string, len(roleBindings.Items))
			for i, rb := range roleBindings.Items {
				rbs[i] = rb.Name
			}
			t.Logf("found %d role bindings: %s", len(roleBindings.Items), spew.Sdump(rbs))
			if nsTmplSet, err := memberAwait.WaitForNSTmplSet(t, owner); err == nil {
				t.Logf("associated NSTemplateSet: %s", spew.Sdump(nsTmplSet))
			}
		}
		require.NoError(t, err)
	}
}

func numberOfLimitRanges(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		err := memberAwait.WaitForExpectedNumberOfResources(t, ns.Name, "LimitRanges", number, func() (int, error) {
			limitRanges := &corev1.LimitRangeList{}
			err := memberAwait.Client.List(context.TODO(), limitRanges, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(limitRanges.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfNetworkPolicies(number int) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		err := memberAwait.WaitForExpectedNumberOfResources(t, ns.Name, "NetworkPolicies", number, func() (int, error) {
			nps := &netv1.NetworkPolicyList{}
			err := memberAwait.Client.List(context.TODO(), nps, providerMatchingLabels, client.InNamespace(ns.Name))
			require.NoError(t, err)
			return len(nps.Items), err
		})
		require.NoError(t, err)
	}
}

func numberOfClusterResourceQuotas(number int) clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			err := memberAwait.WaitForExpectedNumberOfClusterResources(t, "ClusterResourceQuotas", number, func() (int, error) {
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		// TODO fix for migration/existing namespaces cases
		labelWaitCriterion := []wait.LabelWaitCriterion{}
		if !strings.HasPrefix(ns.Name, "migration-") {
			labelWaitCriterion = append(labelWaitCriterion, wait.UntilObjectHasLabel("argocd.argoproj.io/managed-by", "gitops-service-argocd"))
		}
		_, err := memberAwait.WaitForNamespaceWithName(t, ns.Name, labelWaitCriterion...)
		require.NoError(t, err)
	}
}

func appstudioUserActionsRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "appstudio-user-actions")
		require.NoError(t, err)
		assert.Len(t, role.Rules, 13)
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
					Resources: []string{"promotionruns", "snapshotenvironmentbindings", "snapshots", "environments"},
					Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"enterprisecontractpolicies", "integrationtestscenarios", "releases", "releasestrategies", "releaseplans", "releaseplanadmissions"},
					Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokenbindings", "spiaccesschecks", "spiaccesstokens", "spifilecontentrequests"},
					Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokendataupdates"},
					Verbs:     []string{"create"},
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
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"create", "delete", "get", "list", "watch"},
				},
				{
					APIGroups: []string{"results.tekton.dev"},
					Resources: []string{"results", "records"},
					Verbs:     []string{"get", "list"},
				},
				{
					APIGroups: []string{"singapore.open-cluster-management.io"},
					Resources: []string{"registeredclusters"},
					Verbs:     []string{"create", "get", "list", "watch", "delete"},
				},
				{
					APIGroups: []string{"jvmbuildservice.io"},
					Resources: []string{"jbsconfigs", "artifactbuilds"},
					Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete", "deletecollection"},
				},
				{
					APIGroups:     []string{""},
					Resources:     []string{"serviceaccounts"},
					ResourceNames: []string{"pipeline"},
					Verbs:         []string{"get", "list", "watch", "update", "patch"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, role.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func appstudioUserActionsRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, fmt.Sprintf("appstudio-%s-actions-user", userName))
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "appstudio-user-actions", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func appstudioViewRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, fmt.Sprintf("appstudio-%s-view-user", userName))
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
		assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
		assert.Equal(t, owner, rb.ObjectMeta.Labels["toolchain.dev.openshift.com/owner"])
	}
}

func memberOperatorSaReadRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "member-operator-sa-read")
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
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		role, err := memberAwait.WaitForRole(t, ns, "toolchain-sa-read")
		require.NoError(t, err)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"secrets", "serviceaccounts"},
					Verbs:     []string{"get", "list"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"serviceaccounts/token"},
					Verbs:     []string{"create"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
		assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	}
}
