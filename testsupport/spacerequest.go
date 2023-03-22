package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SpaceRequestOption func(request *toolchainv1alpha1.SpaceRequest)

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

func InNamespace(namespace string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.ObjectMeta.Namespace = namespace
	}
}

func CreateSpaceRequest(t *testing.T, awaitilities wait.Awaitilities, memberName string, opts ...SpaceRequestOption) (*toolchainv1alpha1.SpaceRequest, *toolchainv1alpha1.Space) {
	memberAwait, err := awaitilities.Member(memberName)
	require.NoError(t, err)
	// let's first create a parentSpace
	parentSpace, _, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(memberAwait.ClusterName))
	// wait for the namespace to be provisioned since we will be creating the spacerequest into it.
	parentSpace, err = awaitilities.Host().WaitForSpace(t, parentSpace.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)

	// create the space request in the "default" namespace provisioned by the parentSpace
	spaceRequest := NewSpaceRequest(t, append(opts, InNamespace(GetDefaultNamespace(parentSpace.Status.ProvisionedNamespaces)))...)
	require.NotEmpty(t, spaceRequest)
	err = memberAwait.CreateWithCleanup(t, spaceRequest)
	require.NoError(t, err)

	return spaceRequest, parentSpace
}

// NewSpaceRequest initializes a new SpaceRequest object with the given options.
// By default sets appstudio tier and tenant roles for the cluster to use
func NewSpaceRequest(t *testing.T, opts ...SpaceRequestOption) *toolchainv1alpha1.SpaceRequest {
	namePrefix := NewObjectNamePrefix(t)

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
