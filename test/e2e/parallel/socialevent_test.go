package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/socialevent"
	testsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/test/socialevent"
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

	t.Run("create socialevent with default tiername", func(t *testing.T) {
		// given
		name := commonsocialevent.NewName()
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate30"),
			testsocialevent.WithSpaceTier("base1ns"))

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), event)

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
		name := commonsocialevent.NewName()
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("invalid"),
			testsocialevent.WithSpaceTier("base1ns"))

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), event)

		// then
		require.NoError(t, err)
		event, err = hostAwait.WaitForSocialEvent(name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidUserTierReason,
			Message: "UserTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// given
			event.Spec.UserTier = "deactivate30"

			// when
			err := hostAwait.Client.Update(context.TODO(), event)

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(event.Name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})

	t.Run("create socialevent with invalid space tier name", func(t *testing.T) {
		// given
		name := commonsocialevent.NewName()
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate30"),
			testsocialevent.WithSpaceTier("invalid"))

		// when
		err := hostAwait.CreateWithCleanup(context.TODO(), event)

		// then
		require.NoError(t, err)
		event, err = hostAwait.WaitForSocialEvent(name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidSpaceTierReason,
			Message: "NSTemplateTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// given
			event.Spec.SpaceTier = "base"

			// when
			err := hostAwait.Client.Update(context.TODO(), event)

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(event.Name, UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})
}
