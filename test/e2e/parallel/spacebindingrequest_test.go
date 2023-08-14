package parallel

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/gofrs/uuid"

	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateSpaceBindingRequest(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("success", func(t *testing.T) {
		t.Run("create space binding request", func(t *testing.T) {
			// when
			// we create a space to share , a new MUR and a SpaceBindingRequest
			space, spaceBindingRequest, spaceBinding := NewSpaceBindingRequest(t, awaitilities, memberAwait, hostAwait, "admin")

			t.Run("spaceBinding is recreated if deleted ", func(t *testing.T) {
				// now, delete the SpaceBinding,
				// a new SpaceBinding will be provisioned by the SpaceBindingRequest.
				//
				// save the creation timestamp that will be used to ensure that a new SpaceBinding was created with the same name.
				oldSpaceCreationTimeStamp := spaceBinding.CreationTimestamp

				// when
				err := hostAwait.Client.Delete(context.TODO(), spaceBinding)

				// then
				// a new SpaceBinding is created
				// with the same name but creation timestamp should be greater (more recent).
				require.NoError(t, err)
				spaceBinding, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
					UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
					UntilSpaceBindingHasSpaceName(space.Name),
					UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
					UntilSpaceBindingHasCreationTimestampGreaterThan(oldSpaceCreationTimeStamp.Time),
					UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
					UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
				)
				require.NoError(t, err)

				t.Run("SpaceBinding always reflects values from spaceBindingRequest ", func(t *testing.T) {
					// given
					// something/someone updates the SpaceRole directly on the SpaceBinding object

					// when
					spaceBinding, err = hostAwait.UpdateSpaceBinding(t, spaceBinding.Name, func(s *toolchainv1alpha1.SpaceBinding) {
						s.Spec.SpaceRole = "invalidRole" // let's change the role
					})
					require.NoError(t, err)

					// then
					// spaceBindingRequest should reset back the SpaceRole
					spaceBinding, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
						UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
						UntilSpaceBindingHasSpaceName(space.Name),
						UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole), // should have back the role from the SBR
						UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
						UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
					)
					require.NoError(t, err)

					t.Run("delete space binding request", func(t *testing.T) {
						// now, delete the SpaceBindingRequest and expect that the SpaceBinding will be deleted as well,

						// when
						err := memberAwait.Client.Delete(context.TODO(), spaceBindingRequest)

						// then
						// spaceBinding should be deleted as well
						require.NoError(t, err)
						err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
						require.NoError(t, err)
					})
				})
			})
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Run("unable create space binding request with invalid SpaceRole", func(t *testing.T) {
			space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
			// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
			space, err := hostAwait.WaitForSpace(t, space.Name, UntilSpaceHasAnyProvisionedNamespaces())
			require.NoError(t, err)
			// let's create a new MUR that will have access to the space
			username := uuid.Must(uuid.NewV4()).String()
			_, mur := NewSignupRequest(awaitilities).
				Username(username).
				Email(username + "@acme.com").
				ManuallyApprove().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				NoSpace().
				WaitForMUR().Execute(t).Resources()
			// create the spacebinding request
			spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
				WithSpecSpaceRole("invalid"), // set invalid spacerole
				WithSpecMasterUserRecord(mur.GetName()),
				WithNamespace(GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
			)

			// then
			require.NoError(t, err)
			// wait for spacebinding request status to be set
			_, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
				UntilSpaceBindingRequestHasConditions(ProvisioningFailed(fmt.Sprintf("invalid role 'invalid' for space '%s'", space.Name))),
			)
			require.NoError(t, err)
		})

		t.Run("unable create space binding request with invalid MasterUserRecord", func(t *testing.T) {
			space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
			// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
			space, err := hostAwait.WaitForSpace(t, space.Name, UntilSpaceHasAnyProvisionedNamespaces())
			require.NoError(t, err)
			// create the spacebinding request
			spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
				WithSpecSpaceRole("admin"),
				WithSpecMasterUserRecord("invalidMUR"), // we set an invalid MUR
				WithNamespace(GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
			)

			// then
			require.NoError(t, err)
			// wait for spacebinding request status to be set
			_, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
				UntilSpaceBindingRequestHasConditions(ProvisioningFailed("unable to get MUR: MasterUserRecord.toolchain.dev.openshift.com \"invalidMUR\" not found")),
			)
			require.NoError(t, err)
		})
	})
}

func TestUpdateSpaceBindingRequest(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("update space binding request SpaceRole", func(t *testing.T) {
		// when
		space, spaceBindingRequest, _ := NewSpaceBindingRequest(t, awaitilities, memberAwait, hostAwait, "contributor")
		_, err := memberAwait.UpdateSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.Namespace, Name: spaceBindingRequest.Name},
			func(s *toolchainv1alpha1.SpaceBindingRequest) {
				s.Spec.SpaceRole = "admin" // set to admin from contributor
			},
		)
		require.NoError(t, err)

		//then
		// wait for both SpaceBindingRequest and SpaceBinding to have same SpaceRole
		spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
			UntilSpaceBindingRequestHasConditions(Provisioned()),
			UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
			UntilSpaceBindingRequestHasSpecMUR(spaceBindingRequest.Spec.MasterUserRecord),
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
			UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
			UntilSpaceBindingHasSpaceName(space.Name),
			UntilSpaceBindingHasSpaceRole("admin"), // has admin role
			UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
			UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
		)
		require.NoError(t, err)
	})

	t.Run("update space binding request MasterUserRecord", func(t *testing.T) {
		// when
		space, spaceBindingRequest, _ := NewSpaceBindingRequest(t, awaitilities, memberAwait, hostAwait, "admin")
		// let's create another MUR that will have access to the space
		username := uuid.Must(uuid.NewV4()).String()
		_, newmur := NewSignupRequest(awaitilities).
			Username(username).
			Email(username + "@acme.com").
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			NoSpace().
			WaitForMUR().Execute(t).Resources()
		// and we update the MUR in the SBR
		_, err := memberAwait.UpdateSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.Namespace, Name: spaceBindingRequest.Name},
			func(s *toolchainv1alpha1.SpaceBindingRequest) {
				s.Spec.MasterUserRecord = newmur.GetName() // set to the new MUR
			},
		)
		require.NoError(t, err)

		//then
		// wait for both SpaceBindingRequest and SpaceBinding to have same MUR
		spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
			UntilSpaceBindingRequestHasConditions(Provisioned()),
			UntilSpaceBindingRequestHasSpecSpaceRole(spaceBindingRequest.Spec.SpaceRole),
			UntilSpaceBindingRequestHasSpecMUR(newmur.GetName()), // new MUR
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
			UntilSpaceBindingHasMurName(newmur.GetName()), // has new MUR
			UntilSpaceBindingHasSpaceName(space.Name),
			UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
		)
		require.NoError(t, err)
	})
}

func NewSpaceBindingRequest(t *testing.T, awaitilities Awaitilities, memberAwait *MemberAwaitility, hostAwait *HostAwaitility, spaceRole string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBindingRequest, *toolchainv1alpha1.SpaceBinding) {
	space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
	// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
	space, err := hostAwait.WaitForSpace(t, space.Name, UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)
	// let's create a new MUR that will have access to the space
	username := uuid.Must(uuid.NewV4()).String()
	_, mur := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		NoSpace().
		WaitForMUR().Execute(t).Resources()
	// create the spacebinding request
	spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
		WithSpecSpaceRole(spaceRole),
		WithSpecMasterUserRecord(mur.GetName()),
		WithNamespace(GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
	)

	// then
	// check for the spaceBinding creation
	spaceBinding, err := hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
		UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
		UntilSpaceBindingHasSpaceName(space.Name),
		UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
		UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
		UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
	)
	require.NoError(t, err)
	// wait for spacebinding request status
	spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
		UntilSpaceBindingRequestHasConditions(Provisioned()),
	)
	require.NoError(t, err)
	return space, spaceBindingRequest, spaceBinding
}
