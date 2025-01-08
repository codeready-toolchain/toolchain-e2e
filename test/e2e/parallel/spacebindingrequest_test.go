package parallel

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	spacebindingrequesttestcommon "github.com/codeready-toolchain/toolchain-common/pkg/test/spacebindingrequest"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
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

			t.Run("spaceBinding is recreated if deleted", func(t *testing.T) {
				// now, delete the SpaceBinding,
				// a new SpaceBinding will be provisioned by the SpaceBindingRequest.
				//
				// save the old UID that will be used to ensure that a new SpaceBinding was created with the same name but new UID
				oldUID := spaceBinding.UID

				// when
				err := hostAwait.Client.Delete(context.TODO(), spaceBinding)

				// then
				// a new SpaceBinding is created
				// with the same name but different UID.
				require.NoError(t, err)
				spaceBinding, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
					wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
					wait.UntilSpaceBindingHasSpaceName(space.Name),
					wait.UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
					wait.UntilSpaceBindingHasDifferentUID(oldUID),
					wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
					wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
				)
				require.NoError(t, err)

				t.Run("SpaceBinding always reflects values from spaceBindingRequest", func(t *testing.T) {
					// given
					// something/someone updates the SpaceRole directly on the SpaceBinding object

					// when
					spaceBinding, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SpaceBinding{}).
						Update(spaceBinding.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.SpaceBinding) {
							s.Spec.SpaceRole = "maintainer" // let's change the role
						})
					require.NoError(t, err)

					// then
					// spaceBindingRequest should reset back the SpaceRole
					spaceBinding, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
						wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
						wait.UntilSpaceBindingHasSpaceName(space.Name),
						wait.UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole), // should have back the role from the SBR
						wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
						wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
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
			user1 := NewSignupRequest(awaitilities).
				ManuallyApprove().
				TargetCluster(memberAwait).
				RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
				SpaceTier("appstudio").
				EnsureMUR().
				Execute(t)

			// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
			space, err := hostAwait.WaitForSpace(t, user1.Space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
			require.NoError(t, err)
			// let's create a new MUR that will have access to the space
			user2 := NewSignupRequest(awaitilities).
				ManuallyApprove().
				TargetCluster(memberAwait).
				RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
				NoSpace().
				WaitForMUR().Execute(t)
			// create the spacebinding request
			spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
				WithSpecSpaceRole("invalid"), // set invalid spacerole
				WithSpecMasterUserRecord(user2.MUR.GetName()),
				WithNamespace(testsupportspace.GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
			)

			// then
			// wait for spacebinding request status to be set
			_, err = memberAwait.WaitForSpaceBindingRequest(t, client.ObjectKeyFromObject(spaceBindingRequest),
				wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.UnableToCreateSpaceBinding(fmt.Sprintf("invalid role 'invalid' for space '%s'", space.Name))),
			)
			require.NoError(t, err)
			bindings, err := hostAwait.ListSpaceBindings(space.Name)
			require.NoError(t, err)
			assert.Len(t, bindings, 1)

			t.Run("update SBR to fix invalid SpaceRole", func(t *testing.T) {
				// when
				_, err = wait.For(t, memberAwait.Awaitility, &toolchainv1alpha1.SpaceBindingRequest{}).
					Update(spaceBindingRequest.Name, spaceBindingRequest.Namespace, func(sbr *toolchainv1alpha1.SpaceBindingRequest) {
						sbr.Spec.SpaceRole = "admin"
					})

				// then
				require.NoError(t, err)
				_, err = awaitilities.Host().WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
					wait.UntilSpaceBindingHasSpaceRole("admin"))
				require.NoError(t, err)
			})
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
		_, err := wait.For(t, memberAwait.Awaitility, &toolchainv1alpha1.SpaceBindingRequest{}).
			Update(spaceBindingRequest.Name, spaceBindingRequest.Namespace, func(sbr *toolchainv1alpha1.SpaceBindingRequest) {
				sbr.Spec.SpaceRole = "admin"
			})

		require.NoError(t, err)

		//then
		// wait for both SpaceBindingRequest and SpaceBinding to have same SpaceRole
		spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
			wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
			wait.UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
			wait.UntilSpaceBindingRequestHasSpecMUR(spaceBindingRequest.Spec.MasterUserRecord),
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
			wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
			wait.UntilSpaceBindingHasSpaceName(space.Name),
			wait.UntilSpaceBindingHasSpaceRole("admin"), // has admin role
			wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
			wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
		)
		require.NoError(t, err)
	})

	t.Run("update space binding request MasterUserRecord is denied", func(t *testing.T) {
		// when
		space, spaceBindingRequest, _ := NewSpaceBindingRequest(t, awaitilities, memberAwait, hostAwait, "admin")
		// let's create another MUR that will be used for the update request
		username := uuid.Must(uuid.NewV4()).String()
		newUser := NewSignupRequest(awaitilities).
			Username(username).
			Email(username + "@acme.com").
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			NoSpace().
			WaitForMUR().Execute(t)
		// and we try to update the MUR in the SBR
		// with lower timeout since it will fail as expected
		_, err := memberAwait.WithRetryOptions(wait.TimeoutOption(time.Second*2)).UpdateSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.Namespace, Name: spaceBindingRequest.Name},
			func(s *toolchainv1alpha1.SpaceBindingRequest) {
				s.Spec.MasterUserRecord = newUser.MUR.GetName() // set to the new MUR
			},
		)
		require.Error(t, err) // an error from the validating webhook is expected when trying to update the MUR field
		require.EqualError(t, err, "admission webhook \"users.spacebindingrequests.webhook.sandbox\" denied the request: SpaceBindingRequest.MasterUserRecord field cannot be changed. Consider deleting and creating a new SpaceBindingRequest resource")

		//then
		// wait for both SpaceBindingRequest and SpaceBinding to have same MUR
		spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
			wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
			wait.UntilSpaceBindingRequestHasSpecSpaceRole(spaceBindingRequest.Spec.SpaceRole),
			wait.UntilSpaceBindingRequestHasSpecMUR(spaceBindingRequest.Spec.MasterUserRecord), // MUR should be the same
		)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
			wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord), // MUR should be the same
			wait.UntilSpaceBindingHasSpaceName(space.Name),
			wait.UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
		)
		require.NoError(t, err)
	})
}

func NewSpaceBindingRequest(t *testing.T, awaitilities wait.Awaitilities, memberAwait *wait.MemberAwaitility, hostAwait *wait.HostAwaitility, spaceRole string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBindingRequest, *toolchainv1alpha1.SpaceBinding) {
	user := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		SpaceTier("appstudio").
		EnsureMUR().
		Execute(t)
	firstUserSignup := user.UserSignup
	// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
	space, err := hostAwait.WaitForSpace(t, user.Space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)
	// let's create a new MUR that will have access to the space
	username := uuid.Must(uuid.NewV4()).String()
	secondUser := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		NoSpace().
		WaitForMUR().Execute(t)
	secondUserMUR := secondUser.MUR
	// create the spacebinding request
	spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
		WithSpecSpaceRole(spaceRole),
		WithSpecMasterUserRecord(secondUserMUR.GetName()),
		WithNamespace(testsupportspace.GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
	)

	// then
	// check for the spaceBinding creation
	spaceBinding, err := hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
		wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
		wait.UntilSpaceBindingHasSpaceName(space.Name),
		wait.UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
		wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
		wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
	)
	require.NoError(t, err)
	// wait for spacebinding request status
	spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
	)
	require.NoError(t, err)
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, space.Spec.TierName)
	require.NoError(t, err)
	if spaceRole == "admin" {
		usernamesSorted := []string{firstUserSignup.Status.CompliantUsername, secondUserMUR.Name}
		sort.Strings(usernamesSorted)
		_, err = memberAwait.WaitForNSTmplSet(t, space.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(tier.Spec.SpaceRoles[spaceRole].TemplateRef, usernamesSorted[0], usernamesSorted[1])))
		require.NoError(t, err)
	} else {
		_, err = memberAwait.WaitForNSTmplSet(t, space.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, firstUserSignup.Status.CompliantUsername),
				wait.SpaceRole(tier.Spec.SpaceRoles[spaceRole].TemplateRef, secondUserMUR.Name)))
		require.NoError(t, err)
	}
	testsupportspace.VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
	return space, spaceBindingRequest, spaceBinding
}
