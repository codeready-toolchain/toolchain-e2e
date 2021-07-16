package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/condition"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestMetrics(t *testing.T) {

	t.Run("when user deactivated", func(t *testing.T) {

		hostAwait, memberAwait, member2Await := WaitForDeployments(t)

		// host metrics should be available at this point
		t.Run("verify metrics servers", func(t *testing.T) {

			t.Run("verify host metrics server", func(t *testing.T) {
				VerifyHostMetricsService(t, hostAwait)
			})

			t.Run("verify member metrics server", func(t *testing.T) {
				VerifyMemberMetricsService(t, memberAwait)
			})
		})

		hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Disabled(),
			testconfig.Deactivation().DeactivatingNotificationDays(-1))

		config := hostAwait.GetToolchainConfig()
		require.Equal(t, -1, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		t.Run("verify user deactivation on each member cluster", func(t *testing.T) {
			// Initialize metrics assertion counts
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			// User on member cluster 1
			userSignupMember1, _ := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsusertodeactivate").
				Email("metricsusertodeactivate@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			// User on member cluster 2
			userSignupMember2, _ := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsmetricsusertodeactivate2").
				Email("metricsusertodeactivate2@example.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(member2Await).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")       // userSignupMember1
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // userSignupMember1 is on member1
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")       // userSignupMember2
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", member2Await.ClusterName) // userSignupMember2 is on member2
			})

			userSignup1, err := hostAwait.UpdateUserSignupSpec(userSignupMember1.Name, func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
			require.NoError(t, err)
			t.Logf("user signup '%s' set to deactivated", userSignupMember1.Name)
			userSignup2, err := hostAwait.UpdateUserSignupSpec(userSignupMember2.Name, func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
			require.NoError(t, err)
			t.Logf("user signup '%s' set to deactivated", userSignupMember2.Name)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                 // two more because of deactivated users
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")       // userSignupMember1 deactivated
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // userSignupMember1 deactivated
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")       // userSignupMember2 deactivated
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // userSignupMember1 deactivated
			})

			t.Run("reactivate a deactivated user", func(t *testing.T) {
				_, err := hostAwait.UpdateUserSignupSpec(userSignup1.Name, func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivating(us, false)
					states.SetDeactivated(us, false)
					states.SetApproved(us, true)
				})
				require.NoError(t, err)
				_, err = hostAwait.UpdateUserSignupSpec(userSignup2.Name, func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivating(us, false)
					states.SetDeactivated(us, false)
					states.SetApproved(us, true)
				})
				require.NoError(t, err)

				t.Run("verify metrics", func(t *testing.T) {
					metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)                                            // no change
					metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 4)                                    // two more because of reactivated user
					metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                 // no change
					metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")       // userSignupMember1
					metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // userSignupMember1 is on member1
					metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")       // userSignupMember2
					metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", member2Await.ClusterName) // userSignupMember2 is on member2
				})
			})
		})

		t.Run("tests for tiers with automatic deactivation disabled", func(t *testing.T) {
			// Initialize metrics assertion counts
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			userSignupMember1, murMember1 := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsmetricsusernodeactivate").
				Email("metricsusernodeactivate@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // 1 new signup
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // 1 more approved signup
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // signup not deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com`
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)                  // 1 user on member1 (userSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName)                 // no user on member2
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation

			// Get the base tier that has deactivation disabled
			baseDeactivationDisabledTier, err := hostAwait.WaitForNSTemplateTier("basedeactivationdisabled")
			require.NoError(t, err)

			// Move the user to the new tier without deactivation enabled
			murSyncIndex := MoveUserToTier(t, hostAwait, userSignupMember1.Spec.Username, *baseDeactivationDisabledTier).Spec.UserAccounts[0].SyncIndex
			murMember1, err = hostAwait.WaitForMasterUserRecord(murMember1.Name,
				wait.UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
				wait.UntilMasterUserRecordHasNotSyncIndex(murSyncIndex))
			require.NoError(t, err)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // 1 new signup
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // 1 more approved signup
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // signup not deactivated
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com`
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)                  // 1 user on member1 (userSignupMember1)
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName)                 // no user on member2
				metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation
			})

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
			// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
			// time should test the behaviour of the deactivation controller reconciliation.
			manyManyDaysAgo := 999999999999999
			durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
			updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
			murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = updatedProvisionedTime
			})
			require.NoError(t, err)
			t.Logf("masteruserrecord '%s' provisioned time adjusted", murMember1.Name)

			// Ensure the MUR has the updated ProvisionedTime
			_, err = hostAwait.WaitForMasterUserRecord(murMember1.Name, wait.UntilMasterUserRecordHasProvisionedTime(updatedProvisionedTime))
			require.NoError(t, err)

			// The user should not be deactivated so the MUR should not be deleted, expect an error
			err = hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
			require.Error(t, err)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // no change
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // no change
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // no change
				metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 0)                                             // no change
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)                  // 1 user on member1 (userSignupMember1)
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName)                 // no user on member2
				metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation
			})
		})

		t.Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
			// Initialize metrics assertion counts
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			userSignupMember1, murMember1 := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsmetricsusertoautodeactivate").
				Email("metricsmetricsusertoautodeactivate@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			_, excludedMurMember1 := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsuserdeactivationexcluded").
				Email("metricsuserdeactivationexcluded@excluded.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			// Get the provisioned account's tier
			baseTier, err := hostAwait.WaitForNSTemplateTier("base")
			require.NoError(t, err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
			// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
			// time should test the behaviour of the deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
			require.NoError(t, err)
			t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

			// Use the same method above to change the provisioned time for the excluded user
			excludedMurMember1, err = hostAwait.UpdateMasterUserRecordStatus(excludedMurMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
			require.NoError(t, err)
			t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

			// The non-excluded user should be deactivated
			userSignupMember1, err = hostAwait.WaitForUserSignup(userSignupMember1.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(ApprovedByAdmin(), Deactivated())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
			require.NoError(t, err)
			require.True(t, states.Deactivated(userSignupMember1), "usersignup should be deactivated")

			t.Run("verify metrics", func(t *testing.T) {
				// Only the user with domain not on the exclusion list should be auto-deactivated
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1)
				metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 1)
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")       // userSignupMember1 was deactivated
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")       // deactivationExcludedUserSignupMember1 is still there
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // 1 user left on member1 (deactivationExcludedUserSignupMember1)
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
			})
		})

		t.Run("test deactivating state set OK", func(t *testing.T) {
			// Reset configuration back to 3 days
			hostAwait.UpdateToolchainConfig(
				testconfig.AutomaticApproval().Disabled(),
				testconfig.Deactivation().DeactivatingNotificationDays(3))

			config := hostAwait.GetToolchainConfig()
			require.Equal(t, 3, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

			userSignupMember1, murMember1 := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsusertostartdeactivating").
				Email("metricsusertostartdeactivating@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			// Get the provisioned account's tier
			baseTier, err := hostAwait.WaitForNSTemplateTier("base")
			require.NoError(t, err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
			// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
			// period from the current time and setting this as the provisioned time should test the behaviour of the
			// deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
			require.NoError(t, err)
			t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
				murMember1.Status.ProvisionedTime.String())

			// The user should be set to deactivating, but not deactivated
			_, err = hostAwait.WaitForUserSignup(userSignupMember1.Name, wait.UntilUserSignupHasConditions(
				ConditionSet(Default(), ApprovedByAdmin(), Deactivating())...))
			require.NoError(t, err)

			// Verify resources have been provisioned
			VerifyResourcesProvisionedForSignup(t, hostAwait, userSignupMember1, "base", memberAwait)
		})

		t.Run("test full automatic user deactivation lifecycle", func(t *testing.T) {
			// Set configuration to 3 days
			hostAwait.UpdateToolchainConfig(
				testconfig.AutomaticApproval().Enabled(),
				testconfig.Deactivation().DeactivatingNotificationDays(3))

			hostConfig := hostAwait.GetToolchainConfig().Spec.Host
			require.Equal(t, 3, *hostConfig.Deactivation.DeactivatingNotificationDays)

			// Create a new UserSignup
			userSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsfulldeactivationlifecycle").
				Email("metricsfulldeactivationlifecycle@redhat.com").
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
				Execute().Resources()

			// Wait for the UserSignup to have the desired state
			userSignup, err := hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)

			t.Run("user set to deactivating when provisioned time set in past", func(t *testing.T) {
				// Get the provisioned account's tier
				baseTier, err := hostAwait.WaitForNSTemplateTier("base")
				require.NoError(t, err)

				mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
				require.NoError(t, err)

				// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
				// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
				// period from the current time and setting this as the provisioned time should test the behaviour of the
				// deactivation controller reconciliation.
				tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
				mur, err = hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
				require.NoError(t, err)
				t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
					mur.Status.ProvisionedTime.String())

				// The user should be set to deactivating, but not deactivated
				userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
					ConditionSet(Default(), ApprovedAutomatically(), Deactivating())...))
				require.NoError(t, err)

				// Verify resources have been provisioned
				VerifyResourcesProvisionedForSignup(t, hostAwait, userSignup, "base", memberAwait, member2Await)

				t.Run("user set to deactivated after deactivating", func(t *testing.T) {
					// Set the provisioned time even further back
					tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+4) * time.Hour * 24
					mur, err = hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
						mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
					})
					require.NoError(t, err)
					t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
						mur.Status.ProvisionedTime.String())

					// Set the LastTransitionTime of the status to 3 days in the past
					deactivatingLastTransitionTime := metav1.Time{Time: time.Now().Add(time.Duration(-3) * time.Hour * 24)}

					// Update the LastTransitionTime of the DeactivatingNotificationCreated condition
					newConditions := make([]toolchainv1alpha1.Condition, len(userSignup.Status.Conditions))
					copy(newConditions, userSignup.Status.Conditions)
					for i, c := range newConditions {
						if c.Type == toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated {
							newCondition := toolchainv1alpha1.Condition{
								Type:               c.Type,
								Status:             c.Status,
								LastTransitionTime: deactivatingLastTransitionTime,
								Reason:             c.Reason,
								Message:            c.Message,
								LastUpdatedTime:    c.LastUpdatedTime,
							}
							newConditions[i] = newCondition
							break
						}
					}
					userSignup.Status.Conditions = newConditions

					// Confirm that the LastTransitionTime has been correctly set
					updated, found := condition.FindConditionByType(userSignup.Status.Conditions,
						toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated)
					require.True(t, found)
					require.Equal(t, deactivatingLastTransitionTime, updated.LastTransitionTime)

					// Save the updated UserSignup's Status
					require.NoError(t, hostAwait.Client.Status().Update(context.TODO(), userSignup))

					// Trigger a reconciliation of the deactivation controller by updating the MUR
					// - The SyncIndex property of the UserAccount is intended for the express purpose of triggering
					//   a reconciliation, so we set it to some new unique value here
					syncIndex := uuid.Must(uuid.NewV4()).String()
					mur, err := hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
						mur.Spec.UserAccounts[0].SyncIndex = syncIndex
					})
					require.NoError(t, err)

					// The user should now be set to deactivated
					userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
						wait.UntilUserSignupHasConditions(ConditionSet(ApprovedAutomatically(), Deactivated())...))
					require.NoError(t, err)

					// The MUR should also be deleted
					err = hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
					require.NoError(t, err)
				})
			})
		})
	})

	t.Run("when users reactivated", func(t *testing.T) {

		hostAwait, memberAwait, member2Await := WaitForDeployments(t)
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

		// activate and deactivate a few users, and check the metrics.
		// user-0001 will be activated 1 time
		// user-0002 will be activated 2 times
		// user-0003 will be activated 3 times
		// user-0004 will be activated 4 times

		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})
		usersignups := map[string]*toolchainv1alpha1.UserSignup{}
		for i := 1; i <= 3; i++ {
			username := fmt.Sprintf("user-%04d", i)
			usersignups[username] = CreateAndApproveSignup(t, hostAwait, username, memberAwait.ClusterName)

			for j := 1; j < i; j++ { // deactivate and reactivate as many times as necessary (based on its "number")
				// deactivate the user
				_, err := hostAwait.UpdateUserSignupSpec(usersignups[username].Name, func(usersignup *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(usersignup, true)
				})
				require.NoError(t, err)
				err = hostAwait.WaitUntilMasterUserRecordDeleted(username)
				require.NoError(t, err)
				// reactivate the user
				CreateAndApproveSignup(t, hostAwait, username, memberAwait.ClusterName, WithIdentityID(usersignups[username].Spec.Userid))
			}
		}
		// then verify the value of the `sandbox_users_per_activations` metric
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation

		t.Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
			// given
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			// when deleting the host-operator pod to emulate an operator restart during redeployment.
			err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

			// then check how much time it takes to restart and process all existing resources
			require.NoError(t, err)

			// host metrics should become available again at this point
			_, err = hostAwait.WaitForRouteToBeAvailable(hostAwait.Namespace, "host-operator-metrics-service", "/metrics")
			require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics-service' service to be available")

			// then verify that the metric values "survived" the restart
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
		})

		t.Run("delete usersignups", func(t *testing.T) {
			// given
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			// when deleting user "user-0001"
			err := hostAwait.Client.Delete(context.TODO(), usersignups["user-0001"])

			// then
			require.NoError(t, err)
			// and verify that the values of the `sandbox_users_per_activations` metric
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 has been deleted but metric remains unchanged
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // (unchanged after other usersignup was deleted)
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

			// when deleting user "user-0002"
			err = hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

			// then
			require.NoError(t, err)
			// and verify that the values of the `sandbox_users_per_activations` metric
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // (same offset as above)
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 has been deleted but metric remains unchanged
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

		})
	})

	t.Run("when user banned", func(t *testing.T) {
		var httpClient = HTTPClient

		hostAwait, memberAwait, member2Await := WaitForDeployments(t)

		t.Run("ban provisioned usersignup", func(t *testing.T) {
			// Get metrics assertion helper for testing metrics
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

			// Create a new UserSignup and approve it manually
			userSignup, mur := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
				Username("metricsbanprovisioned").
				Email("metricsbanprovisioned@test.com").
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			// Create the BannedUser resource
			bannedUser := banUser(t, hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

			// Confirm the user is banned
			_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
			require.NoError(t, err)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)
				metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
			})

			t.Run("unban the banned user", func(t *testing.T) {
				// Get metrics assertion helper for testing metrics
				metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

				// Unban the user
				err = hostAwait.Client.Delete(context.TODO(), bannedUser)
				require.NoError(t, err)

				// Confirm the BannedUser is deleted
				err = hostAwait.WaitUntilBannedUserDeleted(bannedUser.Name)
				require.NoError(t, err)

				// Confirm the user is provisioned
				userSignup, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
					wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
				require.NoError(t, err)

				// Confirm the MUR is created
				_, err = hostAwait.WaitForMasterUserRecord(mur.Name)
				require.NoError(t, err)

				t.Run("verify metrics", func(t *testing.T) {
					metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 0)         // unchanged: user signup already existed
					metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // user approved
					metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)   // unchanged: banneduser already existed
					metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")
					metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user provisioned on member1
					metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
				})
			})
		})

		t.Run("manually created usersignup with preexisting banneduser", func(t *testing.T) {
			// Get metrics assertion helper for testing metrics
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

			id := uuid.Must(uuid.NewV4()).String()
			email := "testuser" + id + "@test.com"
			banUser(t, hostAwait, email)

			// For this test, we don't want to create the UserSignup via the registration service
			// Instead, we want to confirm the behaviour when a UserSignup with a banned email address is created manually
			userSignup := NewUserSignup(t, hostAwait, "testuser"+id, email)
			userSignup.Spec.TargetCluster = memberAwait.ClusterName

			// Create the UserSignup via the Kubernetes API
			err := hostAwait.CreateWithCleanup(context.TODO(), userSignup)
			require.NoError(t, err)
			t.Logf("user signup '%s' created", userSignup.Name)

			// Check the UserSignup is created
			userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name)
			require.NoError(t, err)

			// Confirm that the user is banned
			assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
			mur, err := hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
			require.NoError(t, err)
			assert.Nil(t, mur)
			require.NoError(t, err)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 0) // not provisioned because banned before signup
				metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
			})
		})

		t.Run("register new user with preexisting ban", func(t *testing.T) {
			// Get metrics assertion helper for testing metrics
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

			id := uuid.Must(uuid.NewV4()).String()
			email := "testuser" + id + "@test.com"
			banUser(t, hostAwait, email)

			// Get valid generated token for e2e tests. IAT claim is overridden
			// to avoid token used before issued error.
			identity0 := authsupport.NewIdentity()
			emailClaim0 := authsupport.WithEmailClaim(email)
			token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
			require.NoError(t, err)

			route := hostAwait.RegistrationServiceURL

			// Call signup endpoint with a valid token to initiate a signup process
			req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+token0)
			req.Header.Set("content-type", "application/json")

			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			defer Close(t, resp)

			t.Run("verify metrics", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 0)
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 0) // not provisioned because banned before signup
				metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)   // banneduser resource was created before signup
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
			})
		})
	})

	t.Run("when user disabled", func(t *testing.T) {
		hostAwait, memberAwait, _ := WaitForDeployments(t)

		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval())

		// Create UserSignup
		userSignup := CreateAndApproveSignup(t, hostAwait, "janedoe", memberAwait.ClusterName)

		VerifyResourcesProvisionedForSignup(t, hostAwait, userSignup, "base", memberAwait)

		// Get MasterUserRecord
		mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
		require.NoError(t, err)

		// Disable MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = true
		})
		require.NoError(t, err)

		// Wait until the UserAccount status is disabled
		userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
			wait.UntilUserAccountHasConditions(Disabled()))
		require.NoError(t, err)

		// Wait until the MUR status is disabled
		mur, err = hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username,
			wait.UntilMasterUserRecordHasConditions(Disabled(), ProvisionedNotificationCRCreated()))
		require.NoError(t, err)

		// Check that the UserAccount is now set to disabled
		require.True(t, userAccount.Spec.Disabled)

		// Check the User is deleted
		user := &userv1.User{}
		err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))

		// Check the Identity is deleted
		identity := &userv1.Identity{}
		err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(userAccount.Spec.UserID)}, identity)
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))

		t.Run("re-enabled mur", func(t *testing.T) {
			// Re-enable MUR
			mur, err = hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Spec.Disabled = false
			})
			require.NoError(t, err)

			VerifyResourcesProvisionedForSignup(t, hostAwait, userSignup, "base", memberAwait)
		})
	})
}

func banUser(t *testing.T, hostAwait *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	bannedUser := &toolchainv1alpha1.BannedUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.Must(uuid.NewV4()).String(),
			Namespace: hostAwait.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: email,
		},
	}
	err := hostAwait.CreateWithCleanup(context.TODO(), bannedUser)
	require.NoError(t, err)
	return bannedUser
}
