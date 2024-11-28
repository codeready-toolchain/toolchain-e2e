package e2e

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/google/uuid"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreationOfUserAndIdentityIsSkipped(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// member cluster configured to skip user creation to mimic appstudio configuration where user & identity resources are not created
	memberConfigurationWithSkipUserCreation := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.SkipUserCreation(true))
	// configure default space tier to appstudio and apply the member configuration
	hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("appstudio"), testconfig.Members().Default(memberConfigurationWithSkipUserCreation.Spec))

	username := "nouseridentity"
	identityID := uuid.New()

	// create pre-existing user and identity
	preexistingUser := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: username,
		},
		Identities: []string{
			identitypkg.NewIdentityNamingStandard(identityID.String(), "rhd").IdentityName(),
		},
	}
	require.NoError(t, memberAwait.CreateWithCleanup(t, preexistingUser))

	preexistingIdentity := &userv1.Identity{
		ObjectMeta: metav1.ObjectMeta{
			Name: identitypkg.NewIdentityNamingStandard(identityID.String(), "rhd").IdentityName(),
		},
		ProviderName:     "rhd",
		ProviderUserName: username,
		User: corev1.ObjectReference{
			Name: preexistingUser.Name,
			UID:  preexistingUser.UID,
		},
	}
	require.NoError(t, memberAwait.CreateWithCleanup(t, preexistingIdentity))

	// Create and approve signup
	u := NewSignupRequest(awaitilities).
		Username(username).
		IdentityID(identityID).
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	signup := u.UserSignup

	VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, signup, "deactivate30", "appstudio")

	// preexisting user & identity are still there and not modified
	// Verify provisioned User
	user, err := memberAwait.WaitForUser(t, preexistingUser.Name)
	require.NoError(t, err)
	assert.Equal(t, preexistingUser.UID, user.UID)
	assert.NotEqual(t, toolchainv1alpha1.ProviderLabelValue, user.Labels[toolchainv1alpha1.ProviderLabelKey])

	// Verify provisioned Identity
	identity, err := memberAwait.WaitForIdentity(t, preexistingIdentity.Name)
	require.NoError(t, err)
	assert.Equal(t, preexistingIdentity.UID, identity.UID)
	assert.NotEqual(t, toolchainv1alpha1.ProviderLabelValue, identity.Labels[toolchainv1alpha1.ProviderLabelKey])

	t.Run("user and identity stay there when user is deactivated", func(t *testing.T) {
		// when
		userSignup, err := hostAwait.UpdateUserSignup(t, false, signup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
		require.NoError(t, err)

		// Wait until the UserSignup is deactivated
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ManuallyDeactivated())...))
		require.NoError(t, err)

		// then
		// Verify provisioned User
		_, err = memberAwait.WaitForUser(t, preexistingUser.Name)
		require.NoError(t, err)

		// Verify provisioned Identity
		_, err = memberAwait.WaitForIdentity(t, preexistingIdentity.Name)
		require.NoError(t, err)
	})
}
