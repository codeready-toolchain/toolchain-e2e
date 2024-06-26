package space

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
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
		s.ObjectMeta.Namespace = namespace
	}
}

func WithName(name string) SpaceRequestOption {
	return func(s *toolchainv1alpha1.SpaceRequest) {
		s.ObjectMeta.GenerateName = ""
		s.ObjectMeta.Name = name
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

	// wait for the namespace to be provisioned since we will be creating the spacerequest into it.
	parentSpace, err := awaitilities.Host().WaitForSpace(t, user.Space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)

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
		// create a kube client by ready the secret created in the spacerequest namespace
		namespaceAccessClient, adminSecret := NewKubeClientFromSecret(t, cl, nsAccess.SecretRef, spaceRequest.Namespace)

		// check expected labels on the secret
		require.NotEmpty(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestLabelKey])
		require.Equal(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestLabelKey], spaceRequest.GetName())
		require.NotEmpty(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestProvisionedNamespaceLabelKey])
		require.Equal(t, adminSecret.Labels[toolchainv1alpha1.SpaceRequestProvisionedNamespaceLabelKey], nsAccess.Name)

		// validate the kube client has access to the namespace name that's in the spacerequest.Status.Namepsacess[n].Name field
		ValidateKubeClient(t, namespaceAccessClient, nsAccess.Name, &corev1.SecretList{})
	}
}

// NewKubeClientFromSecret reads the kubeconfig from a given secret and create a kube rest client from it. You can supply functions to initialize
// the scheme with which the client will be built.
func NewKubeClientFromSecret(t *testing.T, cl client.Client, secretName, secretNamespace string, schemeAdders ...func(*runtime.Scheme) error) (client.Client, *corev1.Secret) {
	t.Helper()
	adminSecret := &corev1.Secret{}
	// retrieve the secret containing the kubeconfig
	require.NoError(t, cl.Get(context.TODO(), types.NamespacedName{
		Namespace: secretNamespace,
		Name:      secretName,
	}, adminSecret))
	assert.NotEmpty(t, adminSecret.Data["kubeconfig"])
	apiConfig, err := clientcmd.Load(adminSecret.Data["kubeconfig"])
	require.NoError(t, err)
	require.False(t, api.IsConfigEmpty(apiConfig))

	// create a new client with the given kubeconfig
	kubeconfig, err := util.BuildKubernetesRESTConfig(*apiConfig)
	require.NoError(t, err)

	s := runtime.NewScheme()
	builder := append(runtime.SchemeBuilder{}, corev1.AddToScheme)
	builder = append(builder, schemeAdders...)
	require.NoError(t, builder.AddToScheme(s))
	namespaceAccessClient, err := client.New(kubeconfig, client.Options{
		Scheme: s,
	})
	require.NoError(t, err)
	return namespaceAccessClient, adminSecret
}

// ValidateKubeClient validates the the kube client can access the given namespace by listing objects using the provided list instance
// The list is checked to not be empty.
func ValidateKubeClient(t *testing.T, namespaceAccessClient client.Client, namespace string, list client.ObjectList) {
	t.Helper()
	require.NoError(t, namespaceAccessClient.List(context.TODO(), list, client.InNamespace(namespace)))
	require.NotEmpty(t, list)
}
