package migration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"k8s.io/apimachinery/pkg/types"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commoncluster "github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	tsspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

const (
	ProvisionedUser             = "mig-prov"
	DeactivatedUser             = "mig-deact"
	BannedUser                  = "mig-banned"
	AppStudioProvisionedUser    = "mig-appst"
	SecondMemberProvisionedUser = "mig-m2-user"

	ProvisionedAppStudioSpace    = "mig-appst-space"
	SecondMemberProvisionedSpace = "mig-m2-space"
	ProvisionedSpaceRequest      = "mig-space-request"
	ProvisionedParentSpace       = "mig-parent-space"
)

type SetupMigrationRunner struct {
	Awaitilities wait.Awaitilities
	WithCleanup  bool
}

func (r *SetupMigrationRunner) Run(t *testing.T) {
	var wg sync.WaitGroup

	toRun := []func(t *testing.T){
		r.prepareAppStudioProvisionedSpace,
		r.prepareSecondMemberProvisionedSpace,
		r.prepareProvisionedSubspace,
		r.prepareProvisionedUser,
		r.prepareSecondMemberProvisionedUser,
		r.prepareDeactivatedUser,
		r.prepareBannedUser,
		r.prepareAppStudioProvisionedUser}

	for _, funcToRun := range toRun {
		wg.Add(1)
		go func(run func(t *testing.T)) {
			defer wg.Done()
			run(t)
		}(funcToRun)
	}

	wg.Wait()
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, ProvisionedAppStudioSpace, "appstudio", r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, SecondMemberProvisionedSpace, "base", r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) createAndWaitForSpace(t *testing.T, name, tierName string, targetCluster *wait.MemberAwaitility) {
	hostAwait := r.Awaitilities.Host()
	space := testspace.NewSpace(r.Awaitilities.Host().Namespace, name, testspace.WithTierName(tierName), testspace.WithSpecTargetCluster(targetCluster.ClusterName), testspace.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, name))
	err := hostAwait.Client.Create(context.TODO(), space)
	require.NoError(t, err)

	_, _, binding := tsspace.CreateMurWithAdminSpaceBindingForSpace(t, r.Awaitilities, space, r.WithCleanup)

	tier, err := hostAwait.WaitForNSTemplateTier(t, tierName)
	require.NoError(t, err)

	_, err = targetCluster.WaitForNSTmplSet(t, space.Name,
		wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles[binding.Spec.SpaceRole].TemplateRef, binding.Spec.MasterUserRecord)))
	require.NoError(t, err)

	_, err = hostAwait.WaitForSpace(t, space.Name,
		wait.UntilSpaceHasConditions(wait.Provisioned()))
	require.NoError(t, err)
	if r.WithCleanup {
		cleanup.AddCleanTasks(t, r.Awaitilities.Host().Client, space)
	}
}

func (r *SetupMigrationRunner) prepareProvisionedSubspace(t *testing.T) {
	memberAwait := r.Awaitilities.Member2()
	r.createAndWaitForSpace(t, ProvisionedParentSpace, "appstudio-env", memberAwait)

	srClusterRoles := []string{commoncluster.RoleLabel(commoncluster.Tenant)}
	t.Logf("creating space request %v for parent space %v", ProvisionedSpaceRequest, ProvisionedParentSpace)
	spaceRequest := createSpaceRequestForParentSpace(t,
		r.Awaitilities,
		memberAwait.ClusterName,
		ProvisionedParentSpace,
		tsspace.WithName(ProvisionedSpaceRequest),
		tsspace.WithSpecTargetClusterRoles(srClusterRoles),
		tsspace.WithSpecTierName("appstudio-env"))

	_, err := memberAwait.WaitForSpaceRequest(t,
		types.NamespacedName{
			Namespace: spaceRequest.Namespace,
			Name:      spaceRequest.Name,
		},
		wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
	)
	require.NoError(t, err)
}

func createSpaceRequestForParentSpace(t *testing.T, awaitilities wait.Awaitilities, memberName, parent string, opts ...space.SpaceRequestOption) *toolchainv1alpha1.SpaceRequest {
	memberAwait, err := awaitilities.Member(memberName)
	require.NoError(t, err)

	// wait for the namespace to be provisioned since we will be creating the spacerequest into it.
	parentSpace, err := awaitilities.Host().WaitForSpace(t, parent, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)

	// create the space request in the "default" namespace provisioned by the parentSpace
	namespace := space.GetDefaultNamespace(parentSpace.Status.ProvisionedNamespaces)
	spaceRequest := space.NewSpaceRequest(t, append(opts, space.WithNamespace(namespace))...)
	require.NotEmpty(t, spaceRequest)

	// don't cleanup the new spacerequest, since we need it for migration testing
	err = memberAwait.Create(t, spaceRequest)
	require.NoError(t, err)
	return spaceRequest
}

func (r *SetupMigrationRunner) prepareProvisionedUser(t *testing.T) {
	r.prepareUser(t, ProvisionedUser, r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedUser(t *testing.T) {
	r.prepareUser(t, SecondMemberProvisionedUser, r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) prepareDeactivatedUser(t *testing.T) {
	userSignup := r.prepareUser(t, DeactivatedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// deactivate the UserSignup
	userSignup, err := hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
	require.NoError(t, err)
	t.Logf("user signup '%s' set to deactivated", userSignup.Name)

	// verify that MUR is deleted
	err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername) // TODO wait for space deletion too after Space migration is done
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareBannedUser(t *testing.T) {
	userSignup := r.prepareUser(t, BannedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// Create the BannedUser
	bannedUser := testsupport.NewBannedUser(hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])
	err := hostAwait.Client.Create(context.TODO(), bannedUser)
	require.NoError(t, err)

	t.Logf("BannedUser '%s' created", bannedUser.Spec.Email)

	// Confirm the user is banned
	_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
		wait.ContainsCondition(wait.Banned()[0]))
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedUser(t *testing.T) {
	usersignup := r.prepareUser(t, AppStudioProvisionedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// promote to appstudio
	tiers.MoveSpaceToTier(t, hostAwait, usersignup.Status.CompliantUsername, "appstudio")

	t.Logf("user %s was promoted to appstudio tier", AppStudioProvisionedUser)

	// verify that it's promoted
	_, err := r.Awaitilities.Host().WaitForMasterUserRecord(t, usersignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()))
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareUser(t *testing.T, name string, targetCluster *wait.MemberAwaitility) *toolchainv1alpha1.UserSignup {
	requestBuilder := testsupport.NewSignupRequest(r.Awaitilities).
		Username(name).
		UserID(uuid.Must(uuid.NewV4()).String()).
		AccountID(uuid.Must(uuid.NewV4()).String()).
		OriginalSub("original_sub_" + name).
		ManuallyApprove().
		TargetCluster(targetCluster)
	if !r.WithCleanup {
		requestBuilder = requestBuilder.DisableCleanup()
	}

	signup, _ := requestBuilder.
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).
		Resources()
	_, err := r.Awaitilities.Host().WaitForMasterUserRecord(t, signup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()))
	require.NoError(t, err)
	return signup
}
