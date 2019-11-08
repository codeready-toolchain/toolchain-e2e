package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	murtest "github.com/codeready-toolchain/toolchain-common/pkg/test/masteruserrecord"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestE2EFlow(t *testing.T) {
	// given
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, murList)
	defer ctx.Cleanup()

	extraMur := createMasterUserRecord(t, awaitility, ctx, "extrajohn")
	t.Log("extra MasterUserRecord created at start")
	targetCluster := extraMur.Spec.UserAccounts[0].TargetCluster
	mur := murtest.NewMasterUserRecord("johnsmith",
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(targetCluster))

	// when
	err := awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(ctx))

	// then
	require.NoError(t, err)
	t.Logf("MasterUserRecord '%s' created", mur.Name)

	verifyResources(t, awaitility, mur.Name,
		wait.UntilMasterUserRecordHasConditions(isProvisioned()),
		wait.UntilUserAccountHasConditions(isProvisioned()))
	verifyResources(t, awaitility, extraMur.Name,
		wait.UntilMasterUserRecordHasConditions(isProvisioned()),
		wait.UntilUserAccountHasConditions(isProvisioned()))

	t.Run("try to break UserAccount", func(t *testing.T) {

		t.Run("delete user and wait until is recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: mur.Name}, user)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, mur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extraMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete identity and wait until is recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(mur.Spec.UserID)}, identity)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, mur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extraMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete user mapping and wait until is recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: mur.Name}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = awaitility.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, mur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extraMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete identity mapping and wait until is recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(mur.Spec.UserID)}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = awaitility.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, mur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extraMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})
	})

	t.Run("update UserAccount spec when MasterUserRecord spec is modified", func(t *testing.T) {
		// given
		toBeModifiedMur, err := wait.NewHostAwaitility(awaitility).WaitForMasterUserRecord(extraMur.Name)
		require.NoError(t, err)
		murtest.ModifyUaInMur(toBeModifiedMur, targetCluster, murtest.NsLimit("advanced"),
			murtest.TierName("admin"), murtest.Namespace("che", "4321"))

		// when
		err = awaitility.Client.Update(context.TODO(), toBeModifiedMur)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' updated", mur.Name)

		// TODO: verify expected condition when the member operator has a logic that updates NsTemplateSet and its status
		verifyResources(t, awaitility, toBeModifiedMur.Name,
			wait.UntilUserAccountHasConditions(isProvisioned()))
		verifyResources(t, awaitility, mur.Name,
			wait.UntilMasterUserRecordHasConditions(isProvisioned()),
			wait.UntilUserAccountHasConditions(isProvisioned()))
	})

	t.Run("update MasterUserRecord status when UserAccount status is modified", func(t *testing.T) {
		// given
		currentMur, err := wait.NewHostAwaitility(awaitility).WaitForMasterUserRecord(mur.Name)
		require.NoError(t, err)
		userAccount, err := wait.NewMemberAwaitility(awaitility).WaitForUserAccount(mur.Name)
		require.NoError(t, err)
		userAccount.Status.Conditions, _ = condition.AddOrUpdateStatusConditions(
			userAccount.Status.Conditions, coolStatus())

		// when
		err = awaitility.ControllerClient.Status().Update(context.TODO(), userAccount)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' updated", mur.Name)

		verifyResources(t, awaitility, currentMur.Name,
			wait.UntilMasterUserRecordHasConditions(isProvisioned()),
			wait.UntilUserAccountHasConditions(isProvisioned(), coolStatus()))

		extraMur, err = wait.NewHostAwaitility(awaitility).WaitForMasterUserRecord(extraMur.Name)
		require.NoError(t, err)
		// TODO: verify expected condition when the member operator has a logic that updates NsTemplateSet and its status
		verifyResources(t, awaitility, extraMur.Name,
			wait.UntilUserAccountHasConditions(isProvisioned()))
	})

	t.Run("delete MasterUserRecord and expect UserAccount to be deleted", func(t *testing.T) {
		// given
		currentMur, err := wait.NewHostAwaitility(awaitility).WaitForMasterUserRecord(mur.Name)
		require.NoError(t, err)

		// when
		err = awaitility.Client.Delete(context.TODO(), currentMur)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' deleted", mur.Name)

		verifyDeletion(t, awaitility, currentMur)

		extraMur, err = wait.NewHostAwaitility(awaitility).WaitForMasterUserRecord(extraMur.Name)
		require.NoError(t, err)
		verifyResources(t, awaitility, extraMur.Name,
			wait.UntilUserAccountHasConditions(isProvisioned()))
	})
}

func verifyResources(t *testing.T, awaitility *wait.Awaitility, murName string, mixedCriteria ...interface{}) {
	masteruserrecordCriteria := []wait.MasterUserRecordWaitCriterion{}
	useraccountCriteria := []wait.UserAccountWaitCriterion{}
	for _, c := range mixedCriteria {
		switch c := c.(type) {
		case wait.MasterUserRecordWaitCriterion:
			masteruserrecordCriteria = append(masteruserrecordCriteria, c)
		case wait.UserAccountWaitCriterion:
			useraccountCriteria = append(useraccountCriteria, c)
		default:
			t.Fatalf("unknown type of criterion: %T", c)
		}
	}

	hostAwait := wait.NewHostAwaitility(awaitility)

	// first, wait for the MasterUserRecord to exist, no matter its status
	mur, err := hostAwait.WaitForMasterUserRecord(murName)
	require.NoError(t, err)

	// then wait for the associated UserAccount to exist, with the given criteria
	memberAwait := wait.NewMemberAwaitility(awaitility)
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name, append(useraccountCriteria, wait.UntilUserAccountHasSpec(mur.Spec.UserAccounts[0].Spec))...)
	assert.NoError(t, err)

	// and finally, check again the MasterUserRecord with the expected (embedded) UserAccount status, on top of the other criteria
	uaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		TargetCluster:     mur.Spec.UserAccounts[0].TargetCluster,
		UserAccountStatus: userAccount.Status,
	}
	_, err = hostAwait.WaitForMasterUserRecord(mur.Name, append(masteruserrecordCriteria, wait.UntilMasterUserRecordHasUserAccountStatuses(uaStatus))...)
	assert.NoError(t, err)

	verifyUserResources(t, memberAwait, userAccount)
	verifyNSTmplSet(t, memberAwait, userAccount)
}

func verifyUserResources(t *testing.T, awaitility *wait.MemberAwaitility, userAcc *toolchainv1alpha1.UserAccount) {
	_, err := awaitility.WaitForUser(userAcc.Name)
	assert.NoError(t, err)

	_, err = awaitility.WaitForIdentity(toIdentityName(userAcc.Spec.UserID))
	assert.NoError(t, err)
}

func verifyNSTmplSet(t *testing.T, awaitility *wait.MemberAwaitility, userAcc *toolchainv1alpha1.UserAccount) {
	_, err := awaitility.WaitForNSTmplSet(userAcc.Name)
	assert.NoError(t, err)
}

func coolStatus() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionType("CoolType"),
		Status:  corev1.ConditionTrue,
		Reason:  "EverythingIsGood",
		Message: "because our SaaS is cool",
	}
}

func isProvisioned() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Provisioned",
	}
}

func createMasterUserRecord(t *testing.T, awaitility *wait.Awaitility, ctx *framework.TestCtx, name string) *toolchainv1alpha1.MasterUserRecord {
	memberCluster, ok, err := awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(t, err)
	require.True(t, ok, "KubeFedCluster should exist")
	mur := murtest.NewMasterUserRecord(name,
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(memberCluster.Name))

	err = awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(ctx))
	require.NoError(t, err)

	verifyResources(t, awaitility, mur.Name,
		wait.UntilMasterUserRecordHasConditions(isProvisioned()),
		wait.UntilUserAccountHasConditions(isProvisioned()))
	return mur
}

func toIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "rhd", userID)
}

func verifyDeletion(t *testing.T, awaitility *wait.Awaitility, mur *toolchainv1alpha1.MasterUserRecord) {
	hostAwait := wait.NewHostAwaitility(awaitility)
	memberAwait := wait.NewMemberAwaitility(awaitility)

	err := hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
	assert.NoError(t, err, "MasterUserRecord is not deleted")

	err = memberAwait.WaitUntilUserAccountDeleted(mur.Name)
	assert.NoError(t, err, "UserAccount is not deleted")

	err = memberAwait.WaitUntilUserDeleted(mur.Name)
	assert.NoError(t, err, "User is not deleted")

	err = memberAwait.WaitUntilIdentityDeleted(mur.Name)
	assert.NoError(t, err, "Identity is not deleted")
}
