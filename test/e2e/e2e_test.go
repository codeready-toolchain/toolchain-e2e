package e2e

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	consoleURL := memberAwait.GetConsoleURL(t)

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
		currentConfig := hostAwait.GetToolchainConfig(t)
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
			memberConfigurationWithCheRequired := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Che().Required(true))
			hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

			memberAwait.WaitForMemberStatus(t,
				wait.UntilMemberStatusHasConditions(ToolchainStatusComponentsNotReady("[routes]")))

			hostAwait.WaitForToolchainStatus(t,
				wait.UntilToolchainStatusHasConditions(ToolchainStatusComponentsNotReady("[members]"), ToolchainStatusUnreadyNotificationNotCreated()))

			t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
				// change che required flag back to true to resolve the error on the memberstatus
				memberConfigurationWithCheRequired = testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Che().Required(false))
				hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

				VerifyMemberStatus(t, memberAwait, consoleURL)
				VerifyToolchainStatus(t, hostAwait, memberAwait)
			})
		})
	})

	// capture "original" counters in toolchain status before creating new Users/Spaces
	originalToolchainStatus := hostAwait.WaitForToolchainStatus(t,
		wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		wait.UntilToolchainStatusUpdatedAfter(time.Now()))
	originalMursPerDomainCount := originalToolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
	t.Logf("the original MasterUserRecord count: %v", originalMursPerDomainCount)
	originalUserAccountCounts := make(map[string]int, len(originalToolchainStatus.Status.Members))
	for _, m := range originalToolchainStatus.Status.Members {
		originalUserAccountCounts[m.ClusterName] = m.UserAccountCount
	}
	originalSpaceCounts := make(map[string]int, len(originalToolchainStatus.Status.Members))
	for _, m := range originalToolchainStatus.Status.Members {
		originalSpaceCounts[m.ClusterName] = m.SpaceCount
	}

	t.Logf("the original UserAccount counts: %v", originalUserAccountCounts)

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, awaitilities, awaitilities.Member1(), 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup, _ := NewSignupRequest(awaitilities).
		Username(johnsmithName).
		ManuallyApprove().
		TargetCluster(memberAwait.ClusterName).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		// DisableCleanup().
		Execute(t).Resources()

	extrajohnName := "extrajohn"
	johnExtraSignup, _ := NewSignupRequest(awaitilities).
		Username(extrajohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait.ClusterName).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	targetedJohnName := "targetedjohn"
	targetedJohnSignup, _ := NewSignupRequest(awaitilities).
		Username(targetedJohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait2.ClusterName).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	originalSubJohnName := "originalsubjohn"
	originalSubJohnClaim := "originalsub:john"
	originalSubJohnSignup, _ := NewSignupRequest(awaitilities).
		Username(originalSubJohnName).
		OriginalSub(originalSubJohnClaim).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait.ClusterName).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	// Confirm the originalSub property has been set during signup
	require.Equal(t, originalSubJohnClaim, originalSubJohnSignup.Spec.OriginalSub)

	VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, targetedJohnSignup, "deactivate30", "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, originalSubJohnSignup, "deactivate30", "base")

	johnsmithMur := hostAwait.GetMasterUserRecord(t, johnsmithName)
	targetedJohnMur := hostAwait.GetMasterUserRecord(t, targetedJohnName)

	t.Run("try to break UserAccount", func(t *testing.T) {

		t.Run("delete user and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			err = memberAwait.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
		})

		t.Run("delete identity and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(johnSignup.Spec.Userid, "rhd").IdentityName()}, identity)
			require.NoError(t, err)

			// when
			err = memberAwait.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
		})

		t.Run("delete user mapping and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = memberAwait.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
		})

		t.Run("delete identity mapping and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(johnSignup.Spec.Userid, "rhd").IdentityName()}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = memberAwait.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 2)
			templateRefs := tiers.GetTemplateRefs(t, hostAwait, "base")
			for _, ref := range templateRefs.Namespaces {
				ns := memberAwait.WaitForNamespace(t, johnSignup.Spec.Username, ref, "base", wait.UntilNamespaceIsActive())
				namespaces = append(namespaces, ns)
			}
			// when
			for _, ns := range namespaces {
				err := memberAwait.Client.Delete(context.TODO(), ns)
				require.NoError(t, err)
			}

			// then
			// wait for the namespaces to be re-created before validating all other resources to avoid race condition
			for _, ref := range templateRefs.Namespaces {
				memberAwait.WaitForNamespace(t, johnSignup.Spec.Username, ref, "base", wait.UntilNamespaceIsActive())
			}
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")
		})

		t.Run("delete useraccount and expect recreation", func(t *testing.T) {
			// given
			ua := memberAwait.WaitForUserAccount(t, johnSignup.Status.CompliantUsername)
			originalCreationTimestamp := ua.CreationTimestamp

			// when deleting the user account
			deletePolicy := metav1.DeletePropagationForeground
			deleteOpts := &client.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}
			err := memberAwait.Client.Delete(context.TODO(), ua, deleteOpts)
			require.NoError(t, err)
			// useraccount deletion happens very quickly so instead of waiting for the useraccount to
			// have a deletion timestamp, ensure the creation timestamp is updated to a newer timestamp
			memberAwait.WaitForUserAccount(t, ua.Name, wait.UntilUserAccountIsCreatedAfter(originalCreationTimestamp))

			// then verify the recreated user account
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "deactivate30", "base")
		})
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, awaitilities, signups)

		// check if the MUR and UA counts match
		hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()),
			wait.UntilToolchainStatusHasMurCount("external", originalMursPerDomainCount["external"]+9), // 5 "multiple-signup-testuser" + "john" + "extrajohn" + "targetedjohn + originalsubjohn
			wait.UntilToolchainStatusHasUserAccountCount(johnsmithMur.Spec.UserAccounts[0].TargetCluster, originalUserAccountCounts[johnsmithMur.Spec.UserAccounts[0].TargetCluster]+8),
			wait.UntilToolchainStatusHasUserAccountCount(targetedJohnMur.Spec.UserAccounts[0].TargetCluster, originalUserAccountCounts[targetedJohnMur.Spec.UserAccounts[0].TargetCluster]+1),
			wait.UntilToolchainStatusHasSpaceCount(johnsmithMur.Spec.UserAccounts[0].TargetCluster, originalSpaceCounts[johnsmithMur.Spec.UserAccounts[0].TargetCluster]+8),
			wait.UntilToolchainStatusHasSpaceCount(targetedJohnMur.Spec.UserAccounts[0].TargetCluster, originalSpaceCounts[targetedJohnMur.Spec.UserAccounts[0].TargetCluster]+1),
		)
	})

	t.Run("verify Space is not deleted if namespace is not deleted", func(t *testing.T) {
		// given
		laraUserName := "laracroft"
		userNamespace := "laracroft-dev"
		cmName := "test-useraccount-delete-1"

		laraSignUp, _ := NewSignupRequest(awaitilities).
			Username(laraUserName).
			Email("laracroft@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait.ClusterName).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		require.Equal(t, "laracroft", laraSignUp.Status.CompliantUsername)

		VerifyResourcesProvisionedForSignup(t, awaitilities, laraSignUp, "deactivate30", "base")

		VerifySpaceBinding(t, hostAwait, laraUserName, laraUserName, "admin")

		// Create a configmap with finalizer in user's namespace, which will block the deletion of namespace.
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: userNamespace,
				Finalizers: []string{
					"test/finalizer.toolchain.e2e.tests",
				},
			},
			Data: map[string]string{
				"video_game": "Tomb Raider",
			},
		}
		err := memberAwait.Client.Create(context.TODO(), cm)
		require.NoError(t, err)
		cm = memberAwait.WaitForConfigMap(t, userNamespace, cmName)
		require.NotEmpty(t, cm)

		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		// when deleting the userSignup
		err = hostAwait.Client.Delete(context.TODO(), laraSignUp, deleteOpts)
		require.NoError(t, err)

		// then nothing should be deleted yet (because of the CM with its own finalizer)
		nsTmplSet := memberAwait.WaitForNSTmplSet(t, laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(TerminatingNSTemplateSet()))
		require.NotEmpty(t, nsTmplSet)

		// Check that namespace is not deleted and is in Terminating state after 10sec
		memberAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForNamespaceInTerminating(t, userNamespace)

		nsTmplSet = memberAwait.WaitForNSTmplSet(t, laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(TerminatingNSTemplateSet()))
		require.NotEmpty(t, nsTmplSet)

		// UserAccount should be deleted when MUR is deleted
		memberAwait.WaitUntilUserAccountDeleted(t, laraUserName)

		// MUR should be deleted when UserSignup is deleted
		hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, laraUserName)

		// UserSignup should be deleted even though Space and NSTemplateSet are stuck deleting so that
		// the behaviour is consistent for both AppStudio & DevSandbox
		hostAwait.WaitUntilUserSignupDeleted(t, laraSignUp.Name)

		// space should be stuck terminating
		space := hostAwait.WaitForSpace(t, laraUserName, wait.UntilSpaceIsBeingDeleted(), wait.UntilSpaceHasConditions(TerminatingSpace()))
		require.NotEmpty(t, space)

		t.Run("remove finalizer", func(t *testing.T) {
			// when removing the finalizer from the CM
			memberAwait.UpdateConfigMap(t, cm.Namespace, cmName,
				func(cm *corev1.ConfigMap) {
					cm.Finalizers = nil
				})

			// then check remaining resources are deleted
			memberAwait.WaitUntilNamespaceDeleted(t, laraUserName, "dev")
			memberAwait.WaitUntilNSTemplateSetDeleted(t, laraUserName)
			hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, laraUserName)
		})

	})

	t.Run("delete namespaced scoped resources of users and expect recreation", func(t *testing.T) {
		userSignup, _ := NewSignupRequest(awaitilities).
			Username("wonderwoman").
			Email("wonderwoman@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait.ClusterName).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()
		devNs := corev1.Namespace{}
		err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-dev"}, &devNs)
		require.NoError(t, err)

		stageNs := corev1.Namespace{}
		err = memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-stage"}, &stageNs)
		require.NoError(t, err)

		// roles and role bindings defined in the namespace templates
		t.Run("namespace role accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, devNs, "exec-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace rolebinding accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, devNs, "crtadmin-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace role accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, stageNs, "exec-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace rolebinding accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {

			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, stageNs, "crtadmin-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		// roles and role bindings defined in the spacerole templates
		t.Run("space role accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, devNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space rolebinding accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, devNs, "wonderwoman-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space role accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, stageNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space rolebinding accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {

			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, stageNs, "wonderwoman-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "base")
		})
	})

	t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		johnSignup := hostAwait.WaitForUserSignup(t, johnSignup.Name)

		// when
		err := hostAwait.Client.Delete(context.TODO(), johnSignup)
		require.NoError(t, err)

		// then
		t.Logf("usersignup '%s' deleted (resource name='%s')", johnsmithName, johnSignup.Name)
		hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, johnsmithName)
		memberAwait.WaitUntilUserAccountDeleted(t, johnsmithName)
		memberAwait.WaitUntilUserDeleted(t, johnsmithName)
		memberAwait.WaitUntilIdentityDeleted(t, johnsmithName)
		hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, johnsmithName)
		memberAwait.WaitUntilNSTemplateSetDeleted(t, johnsmithName)
		memberAwait.WaitUntilClusterResourceQuotasDeleted(t, johnsmithName)
		memberAwait.WaitUntilNamespaceDeleted(t, johnsmithName, "dev")
		memberAwait.WaitUntilNamespaceDeleted(t, johnsmithName, "stage")

		// also, verify that other user's resource are left intact
		VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "deactivate30", "base")

		// check if the MUR and UA counts match
		hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()),
			wait.UntilToolchainStatusHasMurCount("external", originalMursPerDomainCount["external"]+8), // 5 "multiple-signup-testuser" + "extrajohn" + "targetedjohn + originalsubjohn
			wait.UntilToolchainStatusHasUserAccountCount(johnsmithMur.Spec.UserAccounts[0].TargetCluster, originalUserAccountCounts[johnsmithMur.Spec.UserAccounts[0].TargetCluster]+7),
			wait.UntilToolchainStatusHasUserAccountCount(targetedJohnMur.Spec.UserAccounts[0].TargetCluster, originalUserAccountCounts[targetedJohnMur.Spec.UserAccounts[0].TargetCluster]+1),
			wait.UntilToolchainStatusHasSpaceCount(johnsmithMur.Spec.UserAccounts[0].TargetCluster, originalSpaceCounts[johnsmithMur.Spec.UserAccounts[0].TargetCluster]+7),
			wait.UntilToolchainStatusHasSpaceCount(targetedJohnMur.Spec.UserAccounts[0].TargetCluster, originalSpaceCounts[targetedJohnMur.Spec.UserAccounts[0].TargetCluster]+1),
		)
	})

	t.Run("deactivate UserSignup and ensure that all user and identity resources are deleted", func(t *testing.T) {

		// First confirm that there are actually multiple identities for the UserSignup in question, using the
		// owner label to locate them

		// First, find the MUR
		mur := hostAwait.WaitForMasterUserRecord(t, originalSubJohnSignup.Status.CompliantUsername)

		memberAwait := GetMurTargetMember(t, awaitilities, mur)

		// Then locate the UserAccount
		userAccount := memberAwait.WaitForUserAccount(t, mur.Name)
		require.NotNil(t, userAccount)

		// Once we have the UserAccount we can lookup the identities using the owner label
		identityList := &userv1.IdentityList{}
		err := memberAwait.Client.List(context.TODO(), identityList, listByOwnerLabel(userAccount.Name))
		require.NoError(t, err)

		// We should have exactly two identities
		require.Len(t, identityList.Items, 2)

		// Also lookup the user
		userList := &userv1.UserList{}
		err = memberAwait.Client.List(context.TODO(), userList, listByOwnerLabel(userAccount.Name))
		require.NoError(t, err)

		// We should have exactly one user
		require.Len(t, userList.Items, 1)

		// Now deactivate the UserSignup
		userSignup := hostAwait.UpdateUserSignup(t, originalSubJohnSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})

		// Wait until the UserSignup is deactivated
		hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), ManuallyDeactivated())...))

		// Ensure the first identity is deleted
		memberAwait.WaitUntilIdentityDeleted(t, identityList.Items[0].Name)
		// Ensure the second identity is deleted
		memberAwait.WaitUntilIdentityDeleted(t, identityList.Items[1].Name)
		// Ensure the user is deleted
		memberAwait.WaitUntilUserDeleted(t, userList.Items[0].Name)
	})

}

func listByOwnerLabel(owner string) client.ListOption {
	labels := map[string]string{toolchainv1alpha1.OwnerLabelKey: owner}
	return client.MatchingLabels(labels)
}
