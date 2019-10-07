package e2e

import (
	"context"
	"fmt"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	murtest "github.com/codeready-toolchain/toolchain-common/pkg/test/masteruserrecord"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

func TestE2EFlow(t *testing.T) {
	// given
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	ctx, awaitility := doubles.InitializeOperators(t, murList)
	defer ctx.Cleanup()

	extraMur := createMasterUserRecord(awaitility, ctx, "extrajohn")
	t.Log("extra MasterUserRecord created at start")
	targetCluster := extraMur.Spec.UserAccounts[0].TargetCluster
	mur := murtest.NewMasterUserRecord("johnsmith",
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(targetCluster))

	// when
	err := awaitility.Client.Create(context.TODO(), mur, doubles.CleanupOptions(ctx))

	// then
	require.NoError(t, err)
	t.Logf("MasterUserRecord '%s' created", mur.Name)

	verifyResourcesExpectingToBeProvisioned(awaitility, mur)
	verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)

	t.Run("try to break UserAccount", func(t *testing.T) {
		t.Run("delete user and wait until is recreated", func(t *testing.T) {
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: mur.Name}, user)
			require.NoError(t, err)

			err = awaitility.Client.Delete(context.TODO(), user)
			require.NoError(t, err)

			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
		})

		t.Run("delete identity and wait until is recreated", func(t *testing.T) {
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(mur.Spec.UserID)}, identity)
			require.NoError(t, err)

			err = awaitility.Client.Delete(context.TODO(), identity)
			require.NoError(t, err)

			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
		})

		t.Run("delete user mapping and wait until is recreated", func(t *testing.T) {
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: mur.Name}, user)
			require.NoError(t, err)

			user.Identities = []string{}
			err = awaitility.Client.Update(context.TODO(), user)
			require.NoError(t, err)

			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
		})

		t.Run("delete identity mapping and wait until is recreated", func(t *testing.T) {
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(mur.Spec.UserID)}, identity)
			require.NoError(t, err)

			identity.User = corev1.ObjectReference{Name: "", UID: ""}
			err = awaitility.Client.Update(context.TODO(), identity)
			require.NoError(t, err)

			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
		})
	})

	t.Run("update UserAccount spec when MasterUserRecord spec is modified", func(t *testing.T) {
		// given
		toBeModifiedMur := wait.NewHostAwaitility(awaitility).GetMasterUserRecord(extraMur.Name)
		murtest.ModifyUaInMur(toBeModifiedMur, targetCluster, murtest.NsLimit("advanced"),
			murtest.TierName("admin"), murtest.Namespace("che", "4321"))

		// when
		err := awaitility.Client.Update(context.TODO(), toBeModifiedMur)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' updated", mur.Name)

		// TODO: verify expected condition when the member operator has a logic that updates NsTemplateSet and its status
		verifyResources(awaitility, toBeModifiedMur, nil, expectingUaConditions(toBeProvisioned()))
		verifyResourcesExpectingToBeProvisioned(awaitility, mur)
	})

	t.Run("update MasterUserRecord status when UserAccount status is modified", func(t *testing.T) {
		// given
		currentMur := wait.NewHostAwaitility(awaitility).GetMasterUserRecord(mur.Name)
		userAccount := wait.NewMemberAwaitility(awaitility).GetUserAccount(mur.Name)
		userAccount.Status.Conditions, _ = condition.AddOrUpdateStatusConditions(
			userAccount.Status.Conditions, coolStatus())

		// when
		err := awaitility.ControllerClient.Status().Update(context.TODO(), userAccount)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' updated", mur.Name)

		verifyResources(awaitility, currentMur, expectingMurConditions(toBeProvisioned()),
			expectingUaConditions(toBeProvisioned(), coolStatus()))

		extraMur = wait.NewHostAwaitility(awaitility).GetMasterUserRecord(extraMur.Name)
		// TODO: verify expected condition when the member operator has a logic that updates NsTemplateSet and its status
		verifyResources(awaitility, extraMur, nil, expectingUaConditions(toBeProvisioned()))
	})

	t.Run("delete MasterUserRecord and expect UserAccount to be deleted", func(t *testing.T) {
		// given
		currentMur := wait.NewHostAwaitility(awaitility).GetMasterUserRecord(mur.Name)

		// when
		err := awaitility.Client.Delete(context.TODO(), currentMur)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' deleted", mur.Name)

		verifyDeletion(awaitility, currentMur)
		assert.NoError(t, err)

		extraMur = wait.NewHostAwaitility(awaitility).GetMasterUserRecord(extraMur.Name)
		verifyResources(awaitility, extraMur, nil, expectingUaConditions(toBeProvisioned()))
	})
}

type murConditionsGetter func() []toolchainv1alpha1.Condition
type uaConditionsGetter func() []toolchainv1alpha1.Condition

func expectingMurConditions(murStatusConds ...toolchainv1alpha1.Condition) murConditionsGetter {
	return func() []toolchainv1alpha1.Condition {
		return murStatusConds
	}
}
func expectingUaConditions(murStatusConds ...toolchainv1alpha1.Condition) uaConditionsGetter {
	return func() []toolchainv1alpha1.Condition {
		return murStatusConds
	}
}
func verifyResourcesExpectingToBeProvisioned(awaitility *wait.Awaitility, mur *toolchainv1alpha1.MasterUserRecord) {
	verifyResources(awaitility, mur, expectingMurConditions(toBeProvisioned()), expectingUaConditions(toBeProvisioned()))
}

func verifyResources(awaitility *wait.Awaitility, mur *toolchainv1alpha1.MasterUserRecord,
	expectingMurConds murConditionsGetter, expectingUaCons uaConditionsGetter) {

	hostAwait := wait.NewHostAwaitility(awaitility)
	memberAwait := wait.NewMemberAwaitility(awaitility)
	err := hostAwait.WaitForMasterUserRecord(mur.Name)
	assert.NoError(awaitility.T, err)

	murUserAccount := mur.Spec.UserAccounts[0]
	err = memberAwait.WaitForUserAccount(mur.Name, murUserAccount.Spec, expectingUaCons()...)
	assert.NoError(awaitility.T, err)

	userAccount := memberAwait.GetUserAccount(mur.Name)
	uaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		TargetCluster:     murUserAccount.TargetCluster,
		UserAccountStatus: userAccount.Status,
	}

	if expectingMurConds != nil {
		err = hostAwait.WaitForMurConditions(mur.Name,
			wait.UntilHasUserAccountStatus(uaStatus),
			wait.UntilHasStatusCondition(expectingMurConds()...))
	} else {
		err = hostAwait.WaitForMurConditions(mur.Name,
			wait.UntilHasUserAccountStatus(uaStatus))
	}
	assert.NoError(awaitility.T, err)

	verifyUserResources(memberAwait, userAccount)

}

func verifyUserResources(awaitility *wait.MemberAwaitility, userAcc *toolchainv1alpha1.UserAccount) {
	err := awaitility.WaitForUser(userAcc.Name)
	assert.NoError(awaitility.T, err)

	err = awaitility.WaitForIdentity(toIdentityName(userAcc.Spec.UserID))
	assert.NoError(awaitility.T, err)
}

func coolStatus() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionType("CoolType"),
		Status:  v1.ConditionTrue,
		Reason:  "EverythingIsGood",
		Message: "because our SaaS is cool",
	}
}

func toBeProvisioned() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: v1.ConditionTrue,
		Reason: "Provisioned",
	}
}

func createMasterUserRecord(awaitility *wait.Awaitility, ctx *framework.TestCtx, name string) *toolchainv1alpha1.MasterUserRecord {
	memberCluster, ok, err := awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(awaitility.T, err)
	require.True(awaitility.T, ok, "KubeFedCluster should exist")
	mur := murtest.NewMasterUserRecord(name,
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(memberCluster.Name))

	err = awaitility.Client.Create(context.TODO(), mur, doubles.CleanupOptions(ctx))
	require.NoError(awaitility.T, err)

	verifyResourcesExpectingToBeProvisioned(awaitility, mur)

	return mur
}

func toIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "rhd", userID)
}

func verifyDeletion(awaitility *wait.Awaitility, mur *toolchainv1alpha1.MasterUserRecord) {
	hostAwait := wait.NewHostAwaitility(awaitility)
	memberAwait := wait.NewMemberAwaitility(awaitility)

	err := hostAwait.WaitForDeletedMasterUserRecord(mur.Name)
	assert.NoError(awaitility.T, err, "MasterUserRecord is not deleted")

	err = memberAwait.WaitForDeletedUserAccount(mur.Name)
	assert.NoError(awaitility.T, err, "UserAccount is not deleted")

	err = memberAwait.WaitForDeletedUser(mur.Name)
	assert.NoError(awaitility.T, err, "User is not deleted")

	err = memberAwait.WaitForDeletedIdentity(mur.Name)
	assert.NoError(awaitility.T, err, "Identity is not deleted")
}
