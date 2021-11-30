package e2e

import (
	"context"
	"math/rand"
	"strconv"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestSpace(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	consoleURL := memberAwait.GetConsoleURL()
	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses are valid", func(t *testing.T) {
		t.Run("verify member cluster status", func(t *testing.T) {
			VerifyMemberStatus(t, memberAwait, consoleURL)
		})

		t.Run("verify overall toolchain status", func(t *testing.T) {
			VerifyToolchainStatus(t, hostAwait, memberAwait)
		})
	})

	t.Run("verify MemberOperatorConfigs synced from ToolchainConfig to member clusters", func(t *testing.T) {
		currentConfig := hostAwait.GetToolchainConfig()
		expectedMemberConfiguration := currentConfig.Spec.Members.Default

		t.Run("verify ToolchainConfig has synced status", func(t *testing.T) {
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasSyncedStatus(ToolchainConfigSyncComplete()))
		})
		t.Run("verify MemberOperatorConfig was synced to member 1", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait, wait.UntilMemberConfigMatches(expectedMemberConfiguration))
		})
		t.Run("verify MemberOperatorConfig was synced to member 2", func(t *testing.T) {
			member2ExpectedConfig := testconfig.NewMemberOperatorConfigObj(testconfig.Webhook().Deploy(false))
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait2, wait.UntilMemberConfigMatches(member2ExpectedConfig.Spec))
		})
		t.Run("verify updated toolchainconfig is synced - go to unready", func(t *testing.T) {
			// set the che required flag to true to force an error on the memberstatus (che is not installed in e2e test environments)
			memberConfigurationWithCheRequired := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(true))
			hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

			err := memberAwait.WaitForMemberStatus(
				wait.UntilMemberStatusHasConditions(ToolchainStatusComponentsNotReady("[routes]")))
			require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to che being required")

			_, err = hostAwait.WaitForToolchainStatus(
				wait.UntilToolchainStatusHasConditions(ToolchainStatusComponentsNotReady("[members]"), ToolchainStatusUnreadyNotificationNotCreated()))
			require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to che being required")

			t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
				// change che required flag back to true to resolve the error on the memberstatus
				memberConfigurationWithCheRequired = testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(false))
				hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

				VerifyMemberStatus(t, memberAwait, consoleURL)
				VerifyToolchainStatus(t, hostAwait, memberAwait)
			})
		})
	})

	memberAwait.WaitForUsersPodsWebhook()
	memberAwait.WaitForAutoscalingBufferApp()
	_, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
		ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")

	rand.Seed(time.Now().UnixNano())

	t.Run("create space", func(t *testing.T) {
		// given
		space := &toolchainv1alpha1.Space{
			ObjectMeta: v1.ObjectMeta{
				Namespace: hostAwait.Namespace,
				Name:      "oddity-" + strconv.Itoa(rand.Int()),
			},
			Spec: toolchainv1alpha1.SpaceSpec{
				TargetCluster: memberAwait.ClusterName,
				TierName:      "base",
			},
		}

		// when
		err := hostAwait.Client.Create(context.TODO(), space)

		// then
		require.NoError(t, err)
		// wait until NSTemplateSet has been created and Space is in `Ready` status
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		templateRefs := tiers.GetTemplateRefs(hostAwait, space.Spec.TierName)
		for _, templateRef := range templateRefs.Namespaces {
			_, err := memberAwait.WaitForNamespace(nsTmplSet.Name, templateRef, nsTmplSet.Spec.TierName)
			require.NoError(t, err)
		}
		space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(Provisioned()))
		require.NoError(t, err)

		t.Run("delete space", func(t *testing.T) {
			// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
			// along with its associated namespace

			// when
			err = hostAwait.Client.Delete(context.TODO(), space)

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(nsTmplSet.Name)
			require.NoError(t, err)
		})
	})

	t.Run("failed to create space", func(t *testing.T) {

		t.Run("missing target member cluster", func(t *testing.T) {
			// given
			space := &toolchainv1alpha1.Space{
				ObjectMeta: v1.ObjectMeta{
					Namespace: hostAwait.Namespace,
					Name:      "oddity-" + strconv.Itoa(rand.Int()),
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					//TargetCluster missing
					TierName: "base",
				},
			}

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(toolchainv1alpha1.Condition{
				Type:    toolchainv1alpha1.ConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  toolchainv1alpha1.SpaceProvisioningFailedReason,
				Message: "unspecified target member cluster",
			}))
			require.NoError(t, err)

			t.Run("delete space", func(t *testing.T) {
				// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
				// along with its associated namespace

				// when
				err = hostAwait.Client.Delete(context.TODO(), space)

				// then
				require.NoError(t, err)
				err = hostAwait.WaitUntilSpaceDeleted(space.Name)
				require.NoError(t, err)
			})
		})

		t.Run("unknown target member cluster", func(t *testing.T) {
			// given
			space := &toolchainv1alpha1.Space{
				ObjectMeta: v1.ObjectMeta{
					Namespace: hostAwait.Namespace,
					Name:      "oddity-" + strconv.Itoa(rand.Int()),
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					TargetCluster: "unknown",
					TierName:      "base",
				},
			}

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(toolchainv1alpha1.Condition{
				Type:    toolchainv1alpha1.ConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  toolchainv1alpha1.SpaceProvisioningFailedReason,
				Message: "unknown target member cluster 'unknown'",
			}))
			require.NoError(t, err)

			t.Run("delete space", func(t *testing.T) {
				// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
				// along with its associated namespace

				// when
				err = hostAwait.Client.Delete(context.TODO(), space)

				// then
				require.NoError(t, err)
				err = hostAwait.WaitUntilSpaceDeleted(space.Name)
				require.NoError(t, err)
			})
		})
	})
}
