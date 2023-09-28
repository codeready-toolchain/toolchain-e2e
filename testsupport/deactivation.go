package testsupport

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

// DeactivateAndCheckUser deactivates the given UserSignups and checks that the MUR is deprovisioned
func DeactivateAndCheckUser(t *testing.T, awaitilities wait.Awaitilities, userSignup *toolchainv1alpha1.UserSignup) *toolchainv1alpha1.UserSignup {
	hostAwait := awaitilities.Host()
	userSignup, err := hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
	require.NoError(t, err)
	t.Logf("user signup '%s' set to deactivated", userSignup.Name)

	err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername)
	require.NoError(t, err)

	// "deactivated"
	notifications, err := hostAwait.WaitForNotifications(t, userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated, 1, wait.UntilNotificationHasConditions(wait.Sent()))
	require.NoError(t, err)
	require.NotEmpty(t, notifications)
	require.Len(t, notifications, 1)
	notification := notifications[0]
	assert.Contains(t, notification.Name, userSignup.Status.CompliantUsername+"-deactivated-")
	assert.Equal(t, userSignup.Namespace, notification.Namespace)
	assert.Equal(t, "userdeactivated", notification.Spec.Template)
	assert.Equal(t, userSignup.Spec.Userid, notification.Spec.Context["UserID"])

	// We wait for the "Approved()" condition status here because it doesn't specify a reason for the approval,
	// and the reason should not be necessary for the purpose of this test.
	userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.DeactivatedWithoutPreDeactivation())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
	require.NoError(t, err)
	require.True(t, states.Deactivated(userSignup), "usersignup should be deactivated")

	err = hostAwait.WaitUntilNotificationsDeleted(t, userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated)
	require.NoError(t, err)

	// Wait for the notification to be deleted because it will likely be deleted before the space. The space will only be deleted after 30 seconds.
	err = awaitilities.Host().WaitUntilSpaceAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername)
	require.NoError(t, err)

	return userSignup
}

// ReactivateAndCheckUser reactivates the given deactivated UserSignup resource and checks that the MUR is provisioned properly
func ReactivateAndCheckUser(t *testing.T, awaitilities wait.Awaitilities, userSignup *toolchainv1alpha1.UserSignup) *toolchainv1alpha1.UserSignup {
	hostAwait := awaitilities.Host()
	err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: userSignup.Namespace,
		Name:      userSignup.Name,
	}, userSignup)
	require.NoError(t, err)

	userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(us *toolchainv1alpha1.UserSignup) {
			states.SetApprovedManually(us, true)
		})
	require.NoError(t, err)
	t.Logf("user signup '%s' reactivated", userSignup.Name)

	userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
	require.NoError(t, err)
	require.False(t, states.Deactivated(userSignup), "usersignup should not be deactivated")

	VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")

	return userSignup
}
