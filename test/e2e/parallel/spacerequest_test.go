package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateSpaceRequest(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberCluster, found, err := hostAwait.GetToolchainCluster(t, cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, found)

	t.Run("create space request", func(t *testing.T) {
		// when
		targetClusterRoles := []string{cluster.RoleLabel(cluster.Tenant)}
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecTargetClusterRoles(targetClusterRoles))

		// then
		// check for the subSpace creation
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			UntilSpaceHasTargetClusterRoles(targetClusterRoles),
			UntilSpaceHasTier("appstudio-env"),
			UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			UntilSpaceRequestHasConditions(Provisioned()),
			UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Spec.APIEndpoint),
			UntilSpaceRequestHasNamespaceAccess(),
		)
		require.NoError(t, err)
		// verify secret containing kubeconfig was provisioned
		kubeClient, namespaceAccessSecret := newKubeClientFromSecret(t, memberAwait, spaceRequest.Namespace, spaceRequest.Status.NamespaceAccess[0].SecretRef)
		// check that kubeconfig is valid
		validateKubeClient(t, kubeClient, spaceRequest)

		t.Run("subSpace is recreated if deleted ", func(t *testing.T) {
			// now, delete the subSpace, along with its associated namespace,
			// but a new Space will be provisioned by the SpaceRequest.

			// when
			err := hostAwait.Client.Delete(context.TODO(), subSpace)

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, subSpace.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(t, subSpace.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(t, subSpace.Name, "appstudio-env")
			require.NoError(t, err)
			// a new subSpace is created
			subSpace, err = awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
				UntilSpaceHasTargetClusterRoles(targetClusterRoles),
				UntilSpaceHasTier("appstudio-env"),
				UntilSpaceHasAnyProvisionedNamespaces(),
			)
			require.NoError(t, err)

			t.Run("subSpace always reflects values from spaceRequest ", func(t *testing.T) {
				// given
				// something/someone updates the tierName directly on the Space object

				// when
				subSpace, err = hostAwait.UpdateSpace(t, subSpace.Name, func(s *toolchainv1alpha1.Space) {
					s.Spec.TierName = "base" // let's change the tier
				})
				require.NoError(t, err)

				// then
				// spaceRequest should reset back the tierName
				_, err = awaitilities.Host().WaitForSpace(t, subSpace.GetName(),
					UntilSpaceHasTier("appstudio-env"), // tierName is back as the one on spaceRequest
					UntilSpaceHasTargetClusterRoles(targetClusterRoles),
					UntilSpaceHasAnyProvisionedNamespaces(),
				)
				require.NoError(t, err)

				t.Run("namespace access secret is recreated if deleted ", func(t *testing.T) {
					// now, delete the namespace access secret
					// when
					err := memberAwait.Client.Delete(context.TODO(), namespaceAccessSecret)

					// then
					require.NoError(t, err)
					err = memberAwait.WaitUntilSecretDeleted(t, namespaceAccessSecret)
					require.NoError(t, err)
					// a new secret should be created
					spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
						UntilSpaceRequestHasConditions(Provisioned()),
						UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Spec.APIEndpoint),
						UntilSpaceRequestHasNamespaceAccess(),
					)
					require.NoError(t, err)
					// verify secret containing kubeconfig was provisioned
					kubeClient, _ = newKubeClientFromSecret(t, memberAwait, spaceRequest.Namespace, spaceRequest.Status.NamespaceAccess[0].SecretRef)
					// check that kubeconfig is valid
					validateKubeClient(t, kubeClient, spaceRequest)

					t.Run("delete space request", func(t *testing.T) {
						// now, delete the SpaceRequest and expect that the Space will be deleted as well,
						// along with its associated namespace

						// when
						err := memberAwait.Client.Delete(context.TODO(), spaceRequest)

						// then
						// subSpace should be deleted as well
						require.NoError(t, err)
						err = memberAwait.WaitUntilNamespaceDeleted(t, subSpace.Name, "appstudio-env")
						require.NoError(t, err)
						err = memberAwait.WaitUntilNSTemplateSetDeleted(t, subSpace.Name)
						require.NoError(t, err)
						err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, subSpace.Name)
						require.NoError(t, err)
					})
				})
			})
		})
	})

	t.Run("subSpace has parentSpace target cluster when target roles are empty", func(t *testing.T) {
		// when
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"))

		// then
		// check for the subSpace creation with same target cluster as parent one
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			UntilSpaceHasTargetClusterRoles([]string(nil)),                     // empty target cluster roles
			UntilSpaceHasStatusTargetCluster(parentSpace.Status.TargetCluster), // subSpace should have same target cluster as parent space
			UntilSpaceHasTier("appstudio-env"),
			UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			UntilSpaceRequestHasConditions(Provisioned()),
			UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Spec.APIEndpoint))
		require.NoError(t, err)

		t.Run("delete space request", func(t *testing.T) {
			// now, delete the SpaceRequest and expect that the Space will be deleted as well,
			// along with its associated namespace

			// when
			err := memberAwait.Client.Delete(context.TODO(), spaceRequest)

			// then
			// subSpace should be deleted as well
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(t, subSpace.Name, "appstudio-env")
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(t, subSpace.Name)
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, subSpace.Name)
			require.NoError(t, err)
		})
	})
}

func TestUpdateSpaceRequest(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// when
	spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
		WithSpecTierName("appstudio"),
		WithSpecTargetClusterRoles([]string{cluster.RoleLabel(cluster.Tenant)}))

	// then
	// check for the subSpace creation
	subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
		UntilSpaceHasAnyTargetClusterSet(),
		UntilSpaceHasTier("appstudio"),
		UntilSpaceHasAnyProvisionedNamespaces(),
	)
	require.NoError(t, err)
	spaceRequestNamespacedName := types.NamespacedName{Namespace: spaceRequest.Namespace, Name: spaceRequest.Name}
	_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
		UntilSpaceRequestHasTierName("appstudio"),
		UntilSpaceRequestHasConditions(Provisioned()),
	)
	require.NoError(t, err)

	VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
		UntilSpaceHasAnyTargetClusterSet(),
		UntilSpaceHasTier(spaceRequest.Spec.TierName),
	)

	t.Run("update space request tierName", func(t *testing.T) {
		// when
		_, err := memberAwait.UpdateSpaceRequest(t, spaceRequestNamespacedName,
			func(s *toolchainv1alpha1.SpaceRequest) {
				s.Spec.TierName = "base"
			},
		)
		require.NoError(t, err)

		//then
		// wait for both spaceRequest and subSpace to have same tierName
		_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
			UntilSpaceRequestHasTierName("base"),
			UntilSpaceRequestHasConditions(Provisioned()),
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpace(t, subSpace.Name,
			UntilSpaceHasTier("base"),
			UntilSpaceHasConditions(Provisioned()))
		require.NoError(t, err)
	})

	t.Run("update space request target cluster roles", func(t *testing.T) {
		// when
		newTargetClusterRoles := append(spaceRequest.Spec.TargetClusterRoles, cluster.RoleLabel("workload"))
		_, err := memberAwait.UpdateSpaceRequest(t, spaceRequestNamespacedName,
			func(s *toolchainv1alpha1.SpaceRequest) {
				s.Spec.TargetClusterRoles = newTargetClusterRoles // let's assume we add a new cluster role label
			},
		)
		require.NoError(t, err)

		//then
		// wait for both spaceRequest and subSpace to have same target cluster roles
		_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
			UntilSpaceRequestHasTargetClusterRoles(newTargetClusterRoles),
			UntilSpaceRequestHasConditions(Provisioned()),
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpace(t, subSpace.Name,
			UntilSpaceHasTargetClusterRoles(newTargetClusterRoles),
			UntilSpaceHasConditions(Provisioned()))
		require.NoError(t, err)
	})
}

func newKubeClientFromSecret(t *testing.T, member *MemberAwaitility, ns, secretName string) (client.Client, *corev1.Secret) {
	adminSecret := &corev1.Secret{}
	// retrieve the secret containing the kubeconfig
	require.NoError(t, member.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ns,
		Name:      secretName,
	}, adminSecret))
	assert.NotEmpty(t, adminSecret.Data["kubeconfig"])
	apiConfig, err := clientcmd.Load(adminSecret.Data["kubeconfig"])
	require.NoError(t, err)
	require.False(t, api.IsConfigEmpty(apiConfig))

	// create a new client with the given kubeconfig
	kubeconfig, err := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	require.NoError(t, err)
	s := scheme.Scheme
	builder := append(runtime.SchemeBuilder{},
		corev1.AddToScheme,
	)
	require.NoError(t, builder.AddToScheme(s))
	namespaceAccessClient, err := client.New(kubeconfig, client.Options{
		Scheme: s,
	})
	require.NoError(t, err)
	return namespaceAccessClient, adminSecret
}

func validateKubeClient(t *testing.T, namespaceAccessClient client.Client, spaceRequest *toolchainv1alpha1.SpaceRequest) {
	// validate for example that the client can list secrets in the namespace
	secretsList := &corev1.SecretList{}
	require.NoError(t, namespaceAccessClient.List(context.TODO(), secretsList, client.InNamespace(spaceRequest.Status.NamespaceAccess[0].Name)))
	require.NotEmpty(t, secretsList)
}
