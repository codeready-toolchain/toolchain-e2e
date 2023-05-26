package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
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
			UntilSpaceRequestHasConditions(wait.Provisioned()),
			UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Spec.APIEndpoint),
			UntilSpaceRequestHasNamespaceAccess(subSpace),
		)
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

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
			UntilSpaceRequestHasConditions(wait.Provisioned()),
			UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Spec.APIEndpoint),
			UntilSpaceRequestHasNamespaceAccess(subSpace))
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

		t.Run("delete space request", func(t *testing.T) {
			// now, delete the SpaceRequest and expect that the Space will be deleted as well,
			// along with its associated namespace

			// when
			err := memberAwait.Client.Delete(context.TODO(), spaceRequest)

			// then
			// subSpace should be deleted as well
			require.NoError(t, err)
			// check that created namespaces secret access are deleted
			err = memberAwait.WaitUntilSecretsDeleted(t, spaceRequest.Namespace,
				client.MatchingLabels{
					toolchainv1alpha1.SpaceRequestLabelKey: spaceRequest.GetName(),
				},
			)
			require.NoError(t, err)
			// provisioned namespace should be deleted
			// and all the other subspace related resources as well.
			err = memberAwait.WaitUntilNamespaceDeleted(t, subSpace.Name, "appstudio-env")
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(t, subSpace.Name)
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, subSpace.Name)
			require.NoError(t, err)
		})
	})
}

// Update scenarios are not supported as of now since:
// - the only tier that works with this controller is `appstudio-env`,
//   the controller tries to create a TokenRequest for the `namespace-manager` SA , and the only tier that has this SA is the `appstudio-env` tier
// - updating the cluster roles in the SpaceRequest doesn't provision the namespace to a different cluster, as of now this feature is not available.
//
//func TestUpdateSpaceRequest(t *testing.T) {
//	// given
//	t.Parallel()
//	// make sure everything is ready before running the actual tests
//	awaitilities := WaitForDeployments(t)
//	hostAwait := awaitilities.Host()
//	memberAwait := awaitilities.Member1()
//
//	// when
//	spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
//		WithSpecTierName("appstudio"),
//		WithSpecTargetClusterRoles([]string{cluster.RoleLabel(cluster.Tenant)}))
//
//	// then
//	// check for the subSpace creation
//	subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
//		UntilSpaceHasAnyTargetClusterSet(),
//		UntilSpaceHasTier("appstudio"),
//		UntilSpaceHasAnyProvisionedNamespaces(),
//	)
//	require.NoError(t, err)
//	VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
//		UntilSpaceHasAnyTargetClusterSet(),
//		UntilSpaceHasTier(spaceRequest.Spec.TierName),
//	)
//	spaceRequestNamespacedName := types.NamespacedName{Namespace: spaceRequest.Namespace, Name: spaceRequest.Name}
//	_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
//		UntilSpaceRequestHasTierName("appstudio"),
//		UntilSpaceRequestHasConditions(wait.Provisioned()),
//		UntilSpaceRequestHasNamespaceAccess(subSpace),
//	)
//	require.NoError(t, err)
//	VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)
//
//	t.Run("update space request tierName", func(t *testing.T) {
//		// when
//		_, err := memberAwait.UpdateSpaceRequest(t, spaceRequestNamespacedName,
//			func(s *toolchainv1alpha1.SpaceRequest) {
//				s.Spec.TierName = "base"
//			},
//		)
//		require.NoError(t, err)
//
//		//then
//		// wait for both spaceRequest and subSpace to have same tierName
//		_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
//			UntilSpaceRequestHasTierName("base"),
//			UntilSpaceRequestHasConditions(wait.Provisioned()),
//			UntilSpaceRequestHasNamespaceAccess(subSpace),
//		)
//		require.NoError(t, err)
//		_, err = hostAwait.WaitForSpace(t, subSpace.Name,
//			UntilSpaceHasTier("base"),
//			UntilSpaceHasConditions(wait.Provisioned()))
//		require.NoError(t, err)
//	})
//
//	t.Run("update space request target cluster roles", func(t *testing.T) {
//		// when
//		newTargetClusterRoles := append(spaceRequest.Spec.TargetClusterRoles, cluster.RoleLabel("workload"))
//		_, err := memberAwait.UpdateSpaceRequest(t, spaceRequestNamespacedName,
//			func(s *toolchainv1alpha1.SpaceRequest) {
//				s.Spec.TargetClusterRoles = newTargetClusterRoles // let's assume we add a new cluster role label
//			},
//		)
//		require.NoError(t, err)
//
//		//then
//		// wait for both spaceRequest and subSpace to have same target cluster roles
//		_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
//			UntilSpaceRequestHasTargetClusterRoles(newTargetClusterRoles),
//			UntilSpaceRequestHasConditions(wait.Provisioned()),
//			UntilSpaceRequestHasNamespaceAccess(subSpace),
//		)
//		require.NoError(t, err)
//		_, err = hostAwait.WaitForSpace(t, subSpace.Name,
//			UntilSpaceHasTargetClusterRoles(newTargetClusterRoles),
//			UntilSpaceHasConditions(wait.Provisioned()))
//		require.NoError(t, err)
//	})
//}
