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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// tier names
	advanced           = "advanced"
	appstudio          = "appstudio"
	appstudioEnv       = "appstudio-env"
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
	providerMatchingLabels = client.MatchingLabels(map[string]string{toolchainv1alpha1.ProviderLabelKey: toolchainv1alpha1.ProviderLabelValue})
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

	case appstudioEnv:
		return &appstudioEnvTierChecks{tierName: appstudioEnv}, nil

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

func (c *customTierChecks) GetExpectedTemplateRefs(_ *testing.T, _ *wait.HostAwaitility) TemplateRefs {
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
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "40Gi"),
		clusterResourceQuotaDeployments("50"),
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
		resourceQuotaComputeDeploy("20", "14Gi", "3", "14Gi"),
		resourceQuotaComputeBuild("20", "14Gi", "3", "14Gi"),
		resourceQuotaStorage("15Gi", "40Gi", "15Gi", "5"),
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
		clusterResourceQuotaDeployments("50"),
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
		clusterResourceQuotaDeployments("50"),
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
		clusterResourceQuotaDeployments("50"),
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
		clusterResourceQuotaCompute(cpuLimit, "1750m", "16Gi", "40Gi"),
		clusterResourceQuotaDeployments("50"),
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
		clusterResourceQuotaCompute(cpuLimit, "1750m", "7Gi", "40Gi"),
		clusterResourceQuotaDeployments("50"),
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

// toolchainLabelsWaitCriterion create a slice of LabelWaitCriterion containing all the required toolchain labels and values.
func toolchainLabelsWaitCriterion(userName string) []wait.LabelWaitCriterion {
	var labelsWaitCriterion []wait.LabelWaitCriterion
	for labelKey, labelValue := range toolchainLabels(userName) {
		labelsWaitCriterion = append(labelsWaitCriterion, wait.UntilObjectHasLabel(labelKey, labelValue))
	}
	return labelsWaitCriterion
}

// toolchainLabels returns a map containing the expected kubernetes labels that a toolchain resource should have.
func toolchainLabels(userName string) map[string]string {
	return map[string]string{
		toolchainv1alpha1.SpaceLabelKey:    userName,
		toolchainv1alpha1.ProviderLabelKey: toolchainv1alpha1.ProviderLabelValue,
	}
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
		clusterResourceQuotaCompute(cpuLimit, "1750m", "16Gi", "40Gi"),
		clusterResourceQuotaDeployments("50"),
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
		resourceQuotaComputeBuild("60", "64Gi", "6", "32Gi"),
		resourceQuotaStorage("50Gi", "50Gi", "50Gi", "12"),
		limitRange("2", "2Gi", "10m", "256Mi"),
		numberOfLimitRanges(1),
		gitOpsServiceLabel(),
		appstudioWorkSpaceNameLabel(),
		environment("development"),
		resourceQuotaAppstudioCrds("512", "512", "512"),
		resourceQuotaAppstudioCrdsBuild("512"),
		resourceQuotaAppstudioCrdsGitops("512", "512", "512", "512", "512"),
		resourceQuotaAppstudioCrdsIntegration("512", "512", "512"),
		resourceQuotaAppstudioCrdsRelease("512", "512", "512", "512", "512"),
		resourceQuotaAppstudioCrdsEnterpriseContract("512"),
		resourceQuotaAppstudioCrdsSPI("512", "512", "512", "512", "512"),
		pipelineServiceAccount(),
		pipelineRunnerRoleBinding(),
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
					appstudioUserActionsRoleBinding(userName, "admin"),
					appstudioViewRoleBinding(userName),
				)
				rolebindings += 2
			}
		case "maintainer":
			checks = append(checks, appstudioMaintainerUserActionsRole())
			roles++
			for _, userName := range usernames {
				checks = append(checks,
					appstudioUserActionsRoleBinding(userName, "maintainer"),
					appstudioViewRoleBinding(userName),
				)
				rolebindings += 2
			}
		case "contributor":
			checks = append(checks, appstudioContributorUserActionsRole())
			roles++
			for _, userName := range usernames {
				checks = append(checks,
					appstudioUserActionsRoleBinding(userName, "contributor"),
					appstudioViewRoleBinding(userName),
				)
				rolebindings += 2
			}
		default:
			return nil, fmt.Errorf("unexpected template name: '%s'", role)
		}
	}
	// also count the roles, rolebindings and service accounts
	checks = append(checks,
		numberOfToolchainRoles(roles),
		numberOfToolchainRoleBindings(rolebindings+1), // +1 for `appstudio-pipelines-runner-rolebinding`
	)
	return checks, nil
}

func (a *appstudioTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "tenant")
	return templateRefs
}

func (a *appstudioTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments("150"),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(0, ""),
		pipelineRunnerClusterRole())
}

type appstudioEnvTierChecks struct {
	tierName string
}

func (a *appstudioEnvTierChecks) GetNamespaceObjectChecks(_ string) []namespaceObjectsCheck {
	checks := []namespaceObjectsCheck{
		resourceQuotaComputeDeploy("20", "32Gi", "1750m", "32Gi"),
		zeroResourceQuotaComputeBuild(),
		resourceQuotaStorage("50Gi", "50Gi", "50Gi", "12"),
		limitRange("2", "2Gi", "10m", "256Mi"),
		numberOfLimitRanges(1),
		namespaceManagerSA(),
		additionalArgocdReadRole(),
		namespaceManagerSaAdditionalArgocdReadRoleBinding(),
		namespaceManagerSaEditRoleBinding(),
		gitOpsServiceLabel(),
		appstudioWorkSpaceNameLabel(),
	}

	checks = append(checks, append(commonNetworkPolicyChecks(), networkPolicyAllowFromCRW(), numberOfNetworkPolicies(6))...)
	return checks
}

func (a *appstudioEnvTierChecks) GetSpaceRoleChecks(spaceRoles map[string][]string) ([]spaceRoleObjectsCheck, error) {
	for role := range spaceRoles {
		switch role {
		case "admin":
			// no permissions granted
		case "maintainer":
			// no permissions granted
		case "contributor":
			//no permissions granted
		default:
			return nil, fmt.Errorf("unexpected template name: '%s'", role)
		}
	}
	// count the roles, rolebindings
	return []spaceRoleObjectsCheck{
		numberOfToolchainRoles(1),
		numberOfToolchainRoleBindings(2), // 2 for `namespace-manager`
	}, nil
}

func (a *appstudioEnvTierChecks) GetExpectedTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility) TemplateRefs {
	templateRefs := GetTemplateRefs(t, hostAwait, a.tierName)
	verifyNsTypes(t, a.tierName, templateRefs, "env")
	return templateRefs
}

func (a *appstudioEnvTierChecks) GetClusterObjectChecks() []clusterObjectsCheck {
	return clusterObjectsChecks(
		clusterResourceQuotaDeployments("150"),
		clusterResourceQuotaReplicas(),
		clusterResourceQuotaRoutes(),
		clusterResourceQuotaJobs(),
		clusterResourceQuotaServices(),
		clusterResourceQuotaBuildConfig(),
		clusterResourceQuotaSecrets(),
		clusterResourceQuotaConfigMap(),
		numberOfClusterResourceQuotas(8),
		idlers(0, "env"))
}

// verifyNsTypes checks that there's a namespace.TemplateRef that begins with `<tier>-<type>` for each given templateRef (and no more, no less)
func verifyNsTypes(t *testing.T, tier string, templateRefs TemplateRefs, expectedNSTypes ...string) {
	require.Len(t, templateRefs.Namespaces, len(expectedNSTypes))
	actualNSTypes := make([]string, len(expectedNSTypes))
	for i, templateRef := range templateRefs.Namespaces {
		actualTier, actualType, err := wait.TierAndType(templateRef)
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
		rb, err := memberAwait.WaitForRoleBinding(t, ns, userName+"-edit", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "edit", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func rbacEditRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, userName+"-rbac-edit", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "rbac-edit", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func crtadminViewRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "crtadmin-view", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "crtadmin-users-view", rb.Subjects[0].Name)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func crtadminPodsRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "crtadmin-pods", toolchainLabelsWaitCriterion(userName)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "Group", rb.Subjects[0].Kind)
		assert.Equal(t, "crtadmin-users-view", rb.Subjects[0].Name)
		assert.Equal(t, "exec-pods", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func execPodsRole() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, userName string) {
		role, err := memberAwait.WaitForRole(t, ns, "exec-pods", toolchainLabelsWaitCriterion(userName)...)
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
	}
}

func rbacEditRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "rbac-edit", toolchainLabelsWaitCriterion(owner)...)
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

func zeroResourceQuotaComputeBuild() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating},
			Hard:   make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard[corev1.ResourceCPU], err = resource.ParseQuantity("0")
		require.NoError(t, err)
		spec.Hard[corev1.ResourceMemory], err = resource.ParseQuantity("0")
		require.NoError(t, err)
		spec.Hard[corev1.ResourcePods], err = resource.ParseQuantity("0")
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

func resourceQuotaAppstudioCrds(applicationsLimit, componentsLimit, componentDetectionQueriesLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/applications.appstudio.redhat.com"], err = resource.ParseQuantity(applicationsLimit)
		require.NoError(t, err)
		spec.Hard["count/components.appstudio.redhat.com"], err = resource.ParseQuantity(componentsLimit)
		require.NoError(t, err)
		spec.Hard["count/componentdetectionqueries.appstudio.redhat.com"], err = resource.ParseQuantity(componentDetectionQueriesLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsBuild(buildpipelineselectorsLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/buildpipelineselectors.appstudio.redhat.com"], err = resource.ParseQuantity(buildpipelineselectorsLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-build", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-build", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsGitops(environmentsLimit, promotionrunsLimit, deploymenttargetclaimsLimit, deploymenttargetclassesLimit, deploymenttargetsLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/environments.appstudio.redhat.com"], err = resource.ParseQuantity(environmentsLimit)
		require.NoError(t, err)
		spec.Hard["count/promotionruns.appstudio.redhat.com"], err = resource.ParseQuantity(promotionrunsLimit)
		require.NoError(t, err)
		spec.Hard["count/deploymenttargetclaims.appstudio.redhat.com"], err = resource.ParseQuantity(deploymenttargetclaimsLimit)
		require.NoError(t, err)
		spec.Hard["count/deploymenttargetclasses.appstudio.redhat.com"], err = resource.ParseQuantity(deploymenttargetclassesLimit)
		require.NoError(t, err)
		spec.Hard["count/deploymenttargets.appstudio.redhat.com"], err = resource.ParseQuantity(deploymenttargetsLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-gitops", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-gitops", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsIntegration(integrationtestscenariosLimit, snapshotsLimit, snapshotenvironmentbindingsLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/integrationtestscenarios.appstudio.redhat.com"], err = resource.ParseQuantity(integrationtestscenariosLimit)
		require.NoError(t, err)
		spec.Hard["count/snapshots.appstudio.redhat.com"], err = resource.ParseQuantity(snapshotsLimit)
		require.NoError(t, err)
		spec.Hard["count/snapshotenvironmentbindings.appstudio.redhat.com"], err = resource.ParseQuantity(snapshotenvironmentbindingsLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-integration", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-integration", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsRelease(releaseplanadmissionsLimit, releaseplansLimit, releasesLimit, releasestrategiesLimit, internalrequestsLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/releaseplanadmissions.appstudio.redhat.com"], err = resource.ParseQuantity(releaseplanadmissionsLimit)
		require.NoError(t, err)
		spec.Hard["count/releaseplans.appstudio.redhat.com"], err = resource.ParseQuantity(releaseplansLimit)
		require.NoError(t, err)
		spec.Hard["count/releases.appstudio.redhat.com"], err = resource.ParseQuantity(releasesLimit)
		require.NoError(t, err)
		spec.Hard["count/releasestrategies.appstudio.redhat.com"], err = resource.ParseQuantity(releasestrategiesLimit)
		require.NoError(t, err)
		spec.Hard["count/internalrequests.appstudio.redhat.com"], err = resource.ParseQuantity(internalrequestsLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-release", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-release", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsEnterpriseContract(enterprisecontractpoliciesLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/enterprisecontractpolicies.appstudio.redhat.com"], err = resource.ParseQuantity(enterprisecontractpoliciesLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-enterprisecontract", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-enterprisecontract", criteria)
		require.NoError(t, err)
	}
}

func resourceQuotaAppstudioCrdsSPI(spiaccesschecksLimit, spiaccesstokenbindingsLimit, spiaccesstokendataupdatesLimit, spiaccesstokensLimit, spifilecontentrequestsLimit string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, _ string) {
		var err error
		spec := corev1.ResourceQuotaSpec{
			Hard: make(map[corev1.ResourceName]resource.Quantity),
		}
		spec.Hard["count/spiaccesschecks.appstudio.redhat.com"], err = resource.ParseQuantity(spiaccesschecksLimit)
		require.NoError(t, err)
		spec.Hard["count/spiaccesstokenbindings.appstudio.redhat.com"], err = resource.ParseQuantity(spiaccesstokenbindingsLimit)
		require.NoError(t, err)
		spec.Hard["count/spiaccesstokendataupdates.appstudio.redhat.com"], err = resource.ParseQuantity(spiaccesstokendataupdatesLimit)
		require.NoError(t, err)
		spec.Hard["count/spiaccesstokens.appstudio.redhat.com"], err = resource.ParseQuantity(spiaccesstokensLimit)
		require.NoError(t, err)
		spec.Hard["count/spifilecontentrequests.appstudio.redhat.com"], err = resource.ParseQuantity(spifilecontentrequestsLimit)
		require.NoError(t, err)

		criteria := resourceQuotaMatches(ns.Name, "appstudio-crds-spi", spec)
		_, err = memberAwait.WaitForResourceQuota(t, ns.Name, "appstudio-crds-spi", criteria)
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
		assert.Equal(t, toolchainv1alpha1.ProviderLabelValue, lr.ObjectMeta.Labels[toolchainv1alpha1.ProviderLabelKey])
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
		assert.Equal(t, toolchainv1alpha1.ProviderLabelValue, np.ObjectMeta.Labels[toolchainv1alpha1.ProviderLabelKey])
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
		assert.Equal(t, toolchainv1alpha1.ProviderLabelValue, np.ObjectMeta.Labels[toolchainv1alpha1.ProviderLabelKey])
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
			idlerWaitCriterion := []wait.IdlerWaitCriterion{
				wait.IdlerHasTier(tierLabel),
				wait.IdlerHasTimeoutSeconds(timeoutSeconds),
			}
			// cast generic labels wait criterion into idler wait criterion
			for expectedKey, expectedValue := range toolchainLabels(userName) {
				idlerWaitCriterion = append(idlerWaitCriterion, wait.IdlerHasLabel(expectedKey, expectedValue))
			}
			for _, nt := range namespaceTypes {
				var idlerName string
				if nt == "" {
					idlerName = userName
				} else {
					idlerName = fmt.Sprintf("%s-%s", userName, nt)
				}
				_, err := memberAwait.WaitForIdler(t, idlerName, idlerWaitCriterion...)
				require.NoError(t, err)
			}

			// Make sure there is no unexpected idlers
			idlers := &toolchainv1alpha1.IdlerList{}
			err := memberAwait.Client.List(context.TODO(), idlers,
				client.MatchingLabels(map[string]string{
					toolchainv1alpha1.ProviderLabelKey: toolchainv1alpha1.ProviderLabelValue,
					toolchainv1alpha1.SpaceLabelKey:    userName,
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-compute", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
			require.NoError(t, err)
		}
	}
}

// crqToolchainLabelsWaitCriterion checks that expected labels are set on the ClusterResourceQuota.
func crqToolchainLabelsWaitCriterion(userName string) wait.ClusterResourceQuotaWaitCriterion {
	return wait.ClusterResourceQuotaWaitCriterion{
		Match: func(actual *quotav1.ClusterResourceQuota) bool {
			for expectedLabelKey, expectedLabelValue := range toolchainLabels(userName) {
				actualVal, found := actual.Labels[expectedLabelKey]
				if !found || expectedLabelValue != actualVal {
					return false
				}
			}
			// all expected labels are matching
			return true
		},
		Diff: func(actual *quotav1.ClusterResourceQuota) string {
			return fmt.Sprintf("unable to match expected labels on ClusterResourceQuota: %s.\n%s", actual.Name, wait.Diff(toolchainLabels(userName), actual.GetLabels()))
		},
	}
}

func clusterResourceQuotaDeployments(pods string) clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("deployments.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("deploymentconfigs.apps")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count(corev1.ResourcePods)], err = resource.ParseQuantity(pods)
			require.NoError(t, err)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-deployments", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-replicas", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaRoutes() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			var err error
			hard := make(map[corev1.ResourceName]resource.Quantity)
			hard[count("routes.route.openshift.io")], err = resource.ParseQuantity("30")
			require.NoError(t, err)
			hard[count("ingresses.extensions")], err = resource.ParseQuantity("30")
			require.NoError(t, err)

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-routes", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-jobs", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-services", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-bc", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-secrets", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
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

			_, err = memberAwait.WaitForClusterResourceQuota(t, fmt.Sprintf("for-%s-cm", userName),
				crqToolchainLabelsWaitCriterion(userName),
				clusterResourceQuotaMatches(userName, tierLabel, hard),
			)
			require.NoError(t, err)
		}
	}
}

func clusterResourceQuotaMatches(userName, tierName string, hard map[corev1.ResourceName]resource.Quantity) wait.ClusterResourceQuotaWaitCriterion {
	return wait.ClusterResourceQuotaWaitCriterion{
		Match: func(actual *quotav1.ClusterResourceQuota) bool {
			expectedQuotaSpec := quotav1.ClusterResourceQuotaSpec{
				Selector: quotav1.ClusterResourceQuotaSelector{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							toolchainv1alpha1.SpaceLabelKey: userName,
						},
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

func numberOfLimitRanges(number int) namespaceObjectsCheck { // nolint:unparam
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
					toolchainv1alpha1.ProviderLabelKey: toolchainv1alpha1.ProviderLabelValue,
					toolchainv1alpha1.SpaceLabelKey:    userName,
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

func appstudioWorkSpaceNameLabel() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {

		labelWaitCriterion := []wait.LabelWaitCriterion{}
		labelWaitCriterion = append(labelWaitCriterion, wait.UntilObjectHasLabel("appstudio.redhat.com/workspace_name", owner))

		_, err := memberAwait.WaitForNamespaceWithName(t, ns.Name, labelWaitCriterion...)
		require.NoError(t, err)
	}
}

func environment(name string) namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		_, err := memberAwait.WaitForEnvironment(t, ns.Name, name, toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
	}
}

func appstudioUserActionsRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "appstudio-user-actions", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, role.Rules, 14)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"applications", "components", "componentdetectionqueries"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"promotionruns", "snapshotenvironmentbindings", "snapshots", "environments"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"deploymenttargets", "deploymenttargetclaims"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"managed-gitops.redhat.com"},
					Resources: []string{"gitopsdeployments", "gitopsdeploymentmanagedenvironments", "gitopsdeploymentrepositorycredentials", "gitopsdeploymentsyncruns"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"tekton.dev"},
					Resources: []string{"pipelineruns"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"results.tekton.dev"},
					Resources: []string{"results", "records", "logs"},
					Verbs:     []string{"get", "list"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"enterprisecontractpolicies", "integrationtestscenarios", "releases", "releasestrategies", "releaseplans", "releaseplanadmissions"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"jvmbuildservice.io"},
					Resources: []string{"jbsconfigs", "artifactbuilds"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokenbindings", "spiaccesschecks", "spiaccesstokens", "spifilecontentrequests", "spiaccesstokendataupdates"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"remotesecrets"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"secrets"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"buildpipelineselectors"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				{
					APIGroups:     []string{""},
					Resources:     []string{"serviceaccounts"},
					ResourceNames: []string{"appstudio-pipeline"},
					Verbs:         []string{"get", "list", "watch", "update", "patch"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
	}
}

func appstudioMaintainerUserActionsRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "appstudio-maintainer-user-actions", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, role.Rules, 15)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"applications", "components", "componentdetectionqueries"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"promotionruns", "snapshotenvironmentbindings", "snapshots", "environments"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"deploymenttargets", "deploymenttargetclaims"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"managed-gitops.redhat.com"},
					Resources: []string{"gitopsdeployments", "gitopsdeploymentmanagedenvironments", "gitopsdeploymentrepositorycredentials", "gitopsdeploymentsyncruns"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"tekton.dev"},
					Resources: []string{"pipelineruns"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"results.tekton.dev"},
					Resources: []string{"results", "records", "logs"},
					Verbs:     []string{"get", "list"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"integrationtestscenarios"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"enterprisecontractpolicies"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"releases", "releasestrategies", "releaseplans"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"releaseplanadmissions"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"jvmbuildservice.io"},
					Resources: []string{"jbsconfigs", "artifactbuilds"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokenbindings", "spiaccesschecks", "spiaccesstokens", "spifilecontentrequests", "spiaccesstokendataupdates"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"remotesecrets"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"buildpipelineselectors"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
	}
}

func appstudioContributorUserActionsRole() spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "appstudio-contributor-user-actions", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, role.Rules, 15)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"applications", "components", "componentdetectionqueries"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"promotionruns", "snapshotenvironmentbindings", "snapshots", "environments"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"deploymenttargets", "deploymenttargetclaims"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"managed-gitops.redhat.com"},
					Resources: []string{"gitopsdeployments", "gitopsdeploymentmanagedenvironments", "gitopsdeploymentrepositorycredentials", "gitopsdeploymentsyncruns"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"tekton.dev"},
					Resources: []string{"pipelineruns"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"results.tekton.dev"},
					Resources: []string{"results", "records", "logs"},
					Verbs:     []string{"get", "list"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"integrationtestscenarios"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"enterprisecontractpolicies"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"releases", "releasestrategies", "releaseplans"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"releaseplanadmissions"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"jvmbuildservice.io"},
					Resources: []string{"jbsconfigs", "artifactbuilds"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"spiaccesstokenbindings", "spiaccesschecks", "spiaccesstokens", "spifilecontentrequests"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"remotesecrets"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"appstudio.redhat.com"},
					Resources: []string{"buildpipelineselectors"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
	}
}

func appstudioUserActionsRoleBinding(userName string, role string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rbName := ""
		roleName := ""
		if role == "admin" {
			roleName = "appstudio-user-actions"
			rbName = fmt.Sprintf("appstudio-%s-actions-user", userName)
		} else {
			roleName = fmt.Sprintf("appstudio-%s-user-actions", role)
			rbName = fmt.Sprintf("appstudio-%s-%s-actions-user", role, userName)
		}
		rb, err := memberAwait.WaitForRoleBinding(t, ns, rbName, toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, roleName, rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func appstudioViewRoleBinding(userName string) spaceRoleObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, fmt.Sprintf("appstudio-%s-view-user", userName), toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "User", rb.Subjects[0].Kind)
		assert.Equal(t, userName, rb.Subjects[0].Name)
		assert.Equal(t, "view", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func namespaceManagerSaEditRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, toolchainv1alpha1.AdminServiceAccountName, toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, toolchainv1alpha1.AdminServiceAccountName, rb.Subjects[0].Name)
		assert.Equal(t, "edit", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func namespaceManagerSaAdditionalArgocdReadRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "additional-argocd-read", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, toolchainv1alpha1.AdminServiceAccountName, rb.Subjects[0].Name)
		assert.Equal(t, "additional-argocd-read", rb.RoleRef.Name)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func additionalArgocdReadRole() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		role, err := memberAwait.WaitForRole(t, ns, "additional-argocd-read", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		expected := &rbacv1.Role{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"authorization.openshift.io", "rbac.authorization.k8s.io"},
					Resources: []string{"roles"},
					Verbs:     []string{"view", "list", "watch"},
				},
				{
					APIGroups: []string{"networking.k8s.io"},
					Resources: []string{"ingressclasses"},
					Verbs:     []string{"view", "list", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"persistentvolumes"},
					Verbs:     []string{"view", "list", "watch"},
				},
			},
		}

		assert.Equal(t, expected.Rules, role.Rules)
	}
}

func namespaceManagerSA() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		_, err := memberAwait.WaitForServiceAccount(t, ns.Name, toolchainv1alpha1.AdminServiceAccountName)
		require.NoError(t, err)
		// fixme: decide if we want to check labels also on serviceaccounts
		//assertExpectedToolchainLabels(t, sa, owner)
	}
}

func pipelineServiceAccount() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		_, err := memberAwait.WaitForServiceAccount(t, ns.Name, "appstudio-pipeline")
		require.NoError(t, err)
		// fixme: decide if we want to check labels also on serviceaccounts
		//assertExpectedToolchainLabels(t, sa, owner)
	}
}

func pipelineRunnerRoleBinding() namespaceObjectsCheck {
	return func(t *testing.T, ns *corev1.Namespace, memberAwait *wait.MemberAwaitility, owner string) {
		rb, err := memberAwait.WaitForRoleBinding(t, ns, "appstudio-pipelines-runner-rolebinding", toolchainLabelsWaitCriterion(owner)...)
		require.NoError(t, err)
		assert.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, "appstudio-pipeline", rb.Subjects[0].Name)
		assert.Equal(t, ns.Name, rb.Subjects[0].Namespace)
		assert.Equal(t, "appstudio-pipelines-runner", rb.RoleRef.Name)
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	}
}

func pipelineRunnerClusterRole() clusterObjectsCheckCreator {
	return func() clusterObjectsCheck {
		return func(t *testing.T, memberAwait *wait.MemberAwaitility, userName, tierLabel string) {
			clusterRole := &rbacv1.ClusterRole{}
			// we don't wait because this should have been already created by OLM as part of the operator deployment
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "appstudio-pipelines-runner"}, clusterRole)
			require.NoError(t, err)
			assert.NotEmpty(t, clusterRole.Rules)
			//	we don't care much about the content, it should be applied and maintained by the OLM
		}
	}
}
