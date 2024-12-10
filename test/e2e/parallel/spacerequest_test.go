package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
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
	memberCluster, found, err := hostAwait.GetToolchainCluster(t, memberAwait.Namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)
	require.True(t, found)

	t.Run("create space request", func(t *testing.T) {
		// when
		targetClusterRoles := []string{cluster.RoleLabel(cluster.Tenant)}
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecTargetClusterRoles(targetClusterRoles),
			WithSpecDisableInheritance(false))

		// then
		// check for the subSpace creation
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
			wait.UntilSpaceHasTier("appstudio-env"),
			wait.UntilSpaceHasDisableInheritance(false),
			wait.UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Status.APIEndpoint),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
		)
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

		t.Run("subSpace is recreated if deleted", func(t *testing.T) {
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
				wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
				wait.UntilSpaceHasTier("appstudio-env"),
				wait.UntilSpaceHasDisableInheritance(false),
				wait.UntilSpaceHasAnyProvisionedNamespaces(),
				wait.UntilSpaceHasCreationTimestampGreaterThan(oldSpaceCreationTimeStamp.Time),
			)
			require.NoError(t, err)

			t.Run("subSpace always reflects values from spaceRequest", func(t *testing.T) {
				// given
				// something/someone updates the tierName directly on the Space object

				// when
				subSpace, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
					Update(subSpace.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
						s.Spec.TierName = "base1ns" // let's change the tier
					})
				require.NoError(t, err)

				// then
				// spaceRequest should reset back the tierName
				_, err = awaitilities.Host().WaitForSpace(t, subSpace.GetName(),
					wait.UntilSpaceHasTier("appstudio-env"), // tierName is back as the one on spaceRequest
					wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
					wait.UntilSpaceHasDisableInheritance(false),
					wait.UntilSpaceHasAnyProvisionedNamespaces(),
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

	t.Run("create space request with DisableInheritance", func(t *testing.T) {
		// when
		targetClusterRoles := []string{cluster.RoleLabel(cluster.Tenant)}
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecTargetClusterRoles(targetClusterRoles),
			WithSpecDisableInheritance(true))

		// then
		// check for the subSpace creation
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
			wait.UntilSpaceHasTier("appstudio-env"),
			wait.UntilSpaceHasDisableInheritance(true),
			wait.UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Status.APIEndpoint),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
			wait.UntilSpaceRequestHasDisableInheritance(true),
		)
		require.NoError(t, err)
		VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)
	})

	t.Run("subSpace has parentSpace target cluster when target roles are empty", func(t *testing.T) {
		// when
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecDisableInheritance(false))

		// then
		// check for the subSpace creation with same target cluster as parent one
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			wait.UntilSpaceHasTargetClusterRoles([]string(nil)),                     // empty target cluster roles
			wait.UntilSpaceHasStatusTargetCluster(parentSpace.Status.TargetCluster), // subSpace should have same target cluster as parent space
			wait.UntilSpaceHasTier("appstudio-env"),
			wait.UntilSpaceHasDisableInheritance(false),
			wait.UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Status.APIEndpoint),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace))
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
		memberCluster2, found, err := hostAwait.GetToolchainCluster(t, memberAwait2.Namespace, toolchainv1alpha1.ConditionReady)
		require.NoError(t, err)
		require.True(t, found)
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberCluster2.Name, testSpc.WithPlacementRoles(cluster.RoleLabel("member-2")))
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("appstudio-env"),
			WithSpecTargetClusterRoles([]string{cluster.RoleLabel("member-2")}), // the target cluster is member-2 while the spacerequest is on member-1
		)

		// then
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			wait.UntilSpaceHasTargetClusterRoles([]string{cluster.RoleLabel("member-2")}), // member-2 target cluster roles
			wait.UntilSpaceHasStatusTargetCluster(memberCluster2.Name),                    // subSpace should have member-2 target cluster
			wait.UntilSpaceHasTier("appstudio-env"),
			wait.UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		spaceRequest, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster2.Status.APIEndpoint),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace))
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

	t.Run("create space request without secret generation", func(t *testing.T) {
		// when
		targetClusterRoles := []string{cluster.RoleLabel(cluster.Tenant)}
		spaceRequest, parentSpace := CreateSpaceRequest(t, awaitilities, memberAwait.ClusterName,
			WithSpecTierName("base1ns"), // base1ns has no service account associated with the provisioned namespaces
			WithSpecTargetClusterRoles(targetClusterRoles))

		// then
		// check for the subSpace creation
		subSpace, err := awaitilities.Host().WaitForSubSpace(t, spaceRequest.Name, spaceRequest.Namespace, parentSpace.GetName(),
			wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
			wait.UntilSpaceHasTier("base1ns"),
			wait.UntilSpaceHasAnyProvisionedNamespaces(),
		)
		require.NoError(t, err)
		subSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		_, err = memberAwait.WaitForSpaceRequest(t, types.NamespacedName{Namespace: spaceRequest.GetNamespace(), Name: spaceRequest.GetName()},
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Status.APIEndpoint),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
			wait.UntilSpaceRequestHasNamespaceAccessWithoutSecretRef(), // check that namespace access is present but without a SecretRef set
		)
		require.NoError(t, err)
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
		wait.UntilSpaceHasAnyTargetClusterSet(),
		wait.UntilSpaceHasTier("appstudio"),
		wait.UntilSpaceHasAnyProvisionedNamespaces(),
	)
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
		wait.UntilSpaceHasAnyTargetClusterSet(),
		wait.UntilSpaceHasTier(spaceRequest.Spec.TierName),
	)
	spaceRequestNamespacedName := types.NamespacedName{Namespace: spaceRequest.Namespace, Name: spaceRequest.Name}
	_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
		wait.UntilSpaceRequestHasTierName("appstudio"),
		wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
		wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
	)
	require.NoError(t, err)
	VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

	t.Run("update space request tierName", func(t *testing.T) {
		// when
		_, err := wait.For(t, memberAwait.Awaitility, &toolchainv1alpha1.SpaceRequest{}).
			Update(spaceRequestNamespacedName.Name, spaceRequestNamespacedName.Namespace,
				func(s *toolchainv1alpha1.SpaceRequest) {
					s.Spec.TierName = "base"
				})
		require.NoError(t, err)

		// then
		// wait for both spaceRequest and subSpace to have same tierName
		subSpace, err = hostAwait.WaitForSpace(t, subSpace.Name,
			wait.UntilSpaceHasTier("base"),
			wait.UntilSpaceHasConditions(wait.Provisioned()),
			wait.UntilSpaceHasExpectedProvisionedNamespacesNumber(2))
		require.NoError(t, err)

		_, err = memberAwait.WaitForSpaceRequest(t, spaceRequestNamespacedName,
			wait.UntilSpaceRequestHasTierName("base"),
			wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
			wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
		)
		require.NoError(t, err)
	})
}
