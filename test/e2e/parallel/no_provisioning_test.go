package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNoProvisioningState(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	user := NewSignupRequest(awaitilities).Execute(t)

	// when
	_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
		Update(user.UserSignup.Name, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
			states.SetApprovedManually(us, true)
			states.SetNoProvisioning(us, true)
		})
	require.NoError(t, err)

	// then
	us, err := hostAwait.WaitForUserSignup(t, user.UserSignup.Name, wait.UntilUserSignupHasConditions(
		append(wait.Default(),
			toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.UserSignupComplete,
				Status: corev1.ConditionTrue,
				Reason: toolchainv1alpha1.UserSignupInNoProvisioningStateReason,
			},
			toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.UserSignupApproved,
				Status: corev1.ConditionFalse,
				Reason: toolchainv1alpha1.UserSignupInNoProvisioningStateReason,
			})...))
	require.NoError(t, err)
	require.Empty(t, us.Status.CompliantUsername)

	murs := &toolchainv1alpha1.MasterUserRecordList{}
	err = hostAwait.Client.List(context.TODO(), murs, client.MatchingLabels{toolchainv1alpha1.OwnerLabelKey: user.UserSignup.Name})
	require.NoError(t, err)
	require.Empty(t, murs.Items)

	spaces := &toolchainv1alpha1.SpaceList{}
	err = hostAwait.Client.List(context.TODO(), spaces, client.MatchingLabels{toolchainv1alpha1.SpaceCreatorLabelKey: user.UserSignup.Name})
	require.NoError(t, err)
	require.Empty(t, spaces.Items)

	t.Run("removing no-provisioning triggers provisioning", func(t *testing.T) {
		// when
		_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(user.UserSignup.Name, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
				states.SetNoProvisioning(us, false)
			})

		// then
		VerifyResourcesProvisionedForSignup(t, awaitilities, us)
	})
}
