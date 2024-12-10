package parallel

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/socialevent"
	testsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/test/socialevent"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateSocialEvent(t *testing.T) {
	// given
	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := testsupport.WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	t.Run("create socialevent with custom settings", func(t *testing.T) {
		// given
		start := time.Now().Add(time.Hour).Round(time.Second)
		end := time.Now().Add(24 * time.Hour).Round(time.Second)
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate30"),
			testsocialevent.WithSpaceTier("base"),
			testsocialevent.WithStartTime(start),
			testsocialevent.WithEndTime(end),
			testsocialevent.WithMaxAttendees(5),
			testsocialevent.WithTargetCluster("member-1"),
		)

		// when
		err := hostAwait.CreateWithCleanup(t, event)

		// then
		require.NoError(t, err)
		event, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:   toolchainv1alpha1.ConditionReady,
			Status: corev1.ConditionTrue,
		}))
		require.NoError(t, err)
		assert.Equal(t, "deactivate30", event.Spec.UserTier)
		assert.Equal(t, "base", event.Spec.SpaceTier)
		assert.Equal(t, start, event.Spec.StartTime.Time)
		assert.Equal(t, end, event.Spec.EndTime.Time)
		assert.Equal(t, 5, event.Spec.MaxAttendees)
		assert.Equal(t, "member-1", event.Spec.TargetCluster)
	})

	t.Run("create socialevent with invalid user tier name", func(t *testing.T) {
		// given
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("invalid"),
			testsocialevent.WithSpaceTier("base"))

		// when
		err := hostAwait.CreateWithCleanup(t, event)

		// then
		require.NoError(t, err)
		event, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidUserTierReason,
			Message: "UserTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// when
			event, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SocialEvent{}).
				Update(event.Name, hostAwait.Namespace,
					func(ev *toolchainv1alpha1.SocialEvent) {
						ev.Spec.UserTier = "deactivate30"
					})

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})

	t.Run("create socialevent with invalid space tier name", func(t *testing.T) {
		// given
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate30"),
			testsocialevent.WithSpaceTier("invalid"))

		// when
		err := hostAwait.CreateWithCleanup(t, event)

		// then
		require.NoError(t, err)
		event, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.SocialEventInvalidSpaceTierReason,
			Message: "NSTemplateTier 'invalid' not found",
		}))
		require.NoError(t, err)

		t.Run("update with valid tier name", func(t *testing.T) {
			// when
			event, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SocialEvent{}).
				Update(event.Name, hostAwait.Namespace,
					func(ev *toolchainv1alpha1.SocialEvent) {
						ev.Spec.SpaceTier = "base"
					})

			// then
			require.NoError(t, err)
			_, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			}))
			require.NoError(t, err)
		})
	})
}
