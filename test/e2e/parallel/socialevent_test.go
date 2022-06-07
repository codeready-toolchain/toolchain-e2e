package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateSocialEvent(t *testing.T) {
	// given
	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := testsupport.WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	t.Run("create socialevent with valid tiername", func(t *testing.T) {
		// given
		name := testsupport.GenerateName("lab")
		se := testsupport.NewSocialEvent(hostAwait.Namespace, name, "base", "base")

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), se)

		// then
		require.NoError(t, err)
		_, err = hostAwait.WaitForSocialEvent(name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:   toolchainv1alpha1.ConditionReady,
			Status: corev1.ConditionTrue,
		}))
		require.NoError(t, err)
	})

	t.Run("create socialevent with invalid user tier name", func(t *testing.T) {
		// given
		name := testsupport.GenerateName("lab")
		se := testsupport.NewSocialEvent(hostAwait.Namespace, name, "invalid", "base")

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), se)

		// then
		require.NoError(t, err)
		se, err = hostAwait.WaitForSocialEvent(name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidUserTierReason,
			Message: "NSTemplateTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// given
			se.Spec.UserTier = "base"

			// when
			err := hostAwait.Client.Update(context.TODO(), se)

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(se.Name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})

	t.Run("create socialevent with invalid space tier name", func(t *testing.T) {
		// given
		name := testsupport.GenerateName("lab")
		se := testsupport.NewSocialEvent(hostAwait.Namespace, name, "base", "invalid")

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), se)

		// then
		require.NoError(t, err)
		se, err = hostAwait.WaitForSocialEvent(name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidSpaceTierReason,
			Message: "NSTemplateTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// given
			se.Spec.SpaceTier = "base"

			// when
			err := hostAwait.Client.Update(context.TODO(), se)

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(se.Name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})
}
