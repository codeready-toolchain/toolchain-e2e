package testsupport

import (
	"strings"
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
	// let's first create a parentSpace
	parentSpace, _, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(memberAwait.ClusterName))
	// wait for the namespace to be provisioned since we will be creating the spacerequest into it.
	parentSpace, err = awaitilities.Host().WaitForSpace(t, parentSpace.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)

	// create the space request in the "default" namespace provisioned by the parentSpace
	spaceRequest := NewSpaceRequest(t, awaitilities, append(opts, InNamespace(GetDefaultNamespace(parentSpace.Status.ProvisionedNamespaces)))...)
	require.NoError(t, err)
	err = memberAwait.CreateWithCleanup(t, spaceRequest)
	require.NoError(t, err)
	// check for the subSpace creation
	subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace,
		wait.UntilSpaceHasAnyTargetClusterSet(),
		wait.UntilSpaceHasAnyTierNameSet(),
		wait.UntilSpaceHasAnyProvisionedNamespaces(),
	)
	require.NoError(t, err)

	return spaceRequest, subSpace
}

// NewSpaceRequest initializes a new SpaceRequest object with the given options.
// By default sets appstudio tier and tenant roles for the cluster to use
func NewSpaceRequest(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceRequestOption) *toolchainv1alpha1.SpaceRequest {
	namePrefix := strings.ToLower(t.Name())
	// Remove all invalid characters
	namePrefix = notAllowedChars.ReplaceAllString(namePrefix, "")

	// Trim if the length exceeds 60 chars (63 is the max)
	if len(namePrefix) > 40 {
		namePrefix = namePrefix[0:40]
	}

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

func GetDefaultNamespace(provisionedNamespaces []toolchainv1alpha1.SpaceNamespace) string {
	for _, namespaceObj := range provisionedNamespaces {
		if namespaceObj.Type == "default" {
			return namespaceObj.Name
		}
	}
	return ""
}
