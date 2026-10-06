package space

import (
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SpaceRequestOption func(request *toolchainv1alpha1.SpaceRequest) // nolint:revive

func WithSpecTargetClusterRoles(clusterRoles []string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.Spec.TargetClusterRoles = clusterRoles
	}
}

func WithSpecTierName(tierName string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.Spec.TierName = tierName
	}
}

func WithSpecDisableInheritance(disableInheritance bool) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.Spec.DisableInheritance = disableInheritance
	}
}

func WithNamespace(namespace string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.Namespace = namespace
	}
}

func WithName(name string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.GenerateName = ""
		s.Name = name
	}
}

func CreateSpaceRequest(t *testing.T, awaitilities wait.Awaitilities, memberName string, opts ...SpaceRequestOption) (*toolchainv1alpha1.SpaceRequest, *toolchainv1alpha1.Space) {
	memberAwait, err := awaitilities.Member(memberName)
	require.NoError(t, err)
	// let's first create a parentSpace
	user := testsupport.NewSignupRequest(awaitilities).
		ManuallyApprove().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		TargetCluster(memberAwait).
		SpaceTier("appstudio").
		EnsureMUR().
		Execute(t)

	parentSpace := user.Space

	// create the space request in the "default" namespace provisioned by the parentSpace
	spaceRequest := NewSpaceRequest(t, append(opts, WithNamespace(GetDefaultNamespace(parentSpace.Status.ProvisionedNamespaces)))...)
	require.NotEmpty(t, spaceRequest)
	err = memberAwait.CreateWithCleanup(t, spaceRequest)
	require.NoError(t, err)

	return spaceRequest, parentSpace
}

// NewSpaceRequest initializes a new SpaceRequest object with the given options.
// By default sets appstudio tier and tenant roles for the cluster to use
func NewSpaceRequest(t *testing.T, opts ...SpaceRequestOption) *toolchainv1alpha1.SpaceRequest {
	namePrefix := util.NewObjectNamePrefix(t)

	spaceRequest := &toolchainv1alpha1.SpaceRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namePrefix + "-",
		},
	}
	for _, apply := range opts {
		apply(spaceRequest)
	}
	return spaceRequest
}

// VerifyNamespaceAccessForSpaceRequest verifies that the secrets in the namespace access list are valid and they can be used to create a kube client.
func VerifyNamespaceAccessForSpaceRequest(t *testing.T, cl client.Client, spaceRequest *toolchainv1alpha1.SpaceRequest) {
	for _, nsAccess := range spaceRequest.Status.NamespaceAccess {
		// create a kube client by reading the secret created in the spacerequest namespace
		namespaceAccessClient, adminSecret := util.NewKubeClientFromSecret(t, cl, nsAccess.SecretRef, spaceRequest.Namespace)

		// check expected labels on the secret
		require.NotEmpty(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestLabelKey])
		assert.Equal(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestLabelKey], spaceRequest.GetName())
		require.NotEmpty(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestProvisionedNamespaceLabelKey])
		assert.Equal(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestProvisionedNamespaceLabelKey], nsAccess.Name)

		// validate the kube client has access to the namespace name that's in the spacerequest.Status.Namepsacess[n].Name field
		util.ValidateKubeClient(t, namespaceAccessClient, nsAccess.Name, &corev1.SecretList{})
	}
}

func VerifyNamespaceName(t *testing.T, spaceRequest *toolchainv1alpha1.SpaceRequest) {
	for _, nsAccess := range spaceRequest.Status.NamespaceAccess {
		// check the namespace name contains the expected prefix:
		// - normal: first 8 characters of the request name
		// - collision-adjusted: first 7 characters (the 8th is replaced by a collision digit)
		if len(spaceRequest.Name) > 8 {
			assert.True(t, strings.Contains(nsAccess.Name, spaceRequest.Name[:8]) || strings.Contains(nsAccess.Name, spaceRequest.Name[:7]),
				"expected namespace %q to contain %q (normal) or %q (collision-adjusted)", nsAccess.Name, spaceRequest.Name[:8], spaceRequest.Name[:7])
		} else {
			assert.Contains(t, nsAccess.Name, spaceRequest.Name)
		}
	}
}
