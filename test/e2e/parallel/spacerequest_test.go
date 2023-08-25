package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
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
	memberAwait2 := awaitilities.Member2()
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
			UntilSpaceRequestHasNamespaceAccess(subSpace),
		)
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

		t.Run("subSpace is recreated if deleted ", func(t *testing.T) {
			// now, delete the subSpace, along with its associated namespace,
			// but a new Space will be provisioned by the SpaceRequest.
			//
			// save the creation timestamp that will be used to ensure that a new subSpace was created with the same name.
			oldSpaceCreationTimeStamp := subSpace.CreationTimestamp

			// when
			err := hostAwait.Client.Delete(context.TODO(), subSpace)

			// then
			// a new subSpace is created
			// with the same name but creation timestamp should be greater (more recent).
			require.NoError(t, err)
			subSpace, err = awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
				UntilSpaceHasTargetClusterRoles(targetClusterRoles),
				UntilSpaceHasTier("appstudio-env"),
				UntilSpaceHasAnyProvisionedNamespaces(),
				UntilSpaceHasCreationTimestampGreaterThan(oldSpaceCreationTimeStamp.Time),
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
			UntilSpaceRequestHasConditions(Provisioned()),
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

	t.Run("subSpace target cluster is different from spacerequest cluster", func(t *testing.T) {
		// when
		// we add a custom cluster-role for member2
		memberCluster2, found, err := hostAwait.GetToolchainCluster(t, memberAwait2.Type, memberAwait2.Namespace, nil)
		require.NoError(t, err)
		require.True(t, found)
		_, err = hostAwait.UpdateToolchainCluster(t, memberCluster2.Name, func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Labels[cluster.RoleLabel("member-2")] = "" // add a new cluster-role label, the value is blank since only key matters.
		})
		require.NoError(t, err)
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecTargetClusterRoles([]string{cluster.RoleLabel("member-2")}), // the target cluster is member-2 while the spacerequest is on member-1
		)

		// then
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			UntilSpaceHasTargetClusterRoles([]string{cluster.RoleLabel("member-2")}), // member-2 target cluster roles
			UntilSpaceHasStatusTargetCluster(memberCluster2.Name),                    // subSpace should have member-2 target cluster
			UntilSpaceHasTier("appstudio-env"),
			UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			UntilSpaceRequestHasConditions(Provisioned()),
			UntilSpaceRequestHasStatusTargetClusterURL(memberCluster2.Spec.APIEndpoint),
			UntilSpaceRequestHasNamespaceAccess(subSpace))
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait2.Client, spaceRequest) // space request has access to ns on member2

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
