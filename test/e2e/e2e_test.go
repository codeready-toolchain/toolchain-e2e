package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	murtest "github.com/codeready-toolchain/toolchain-common/pkg/test/masteruserrecord"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	uuid "github.com/satori/go.uuid"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestMasterUserAccountFlow(t *testing.T) {
	// given
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, murList)
	defer ctx.Cleanup()

	extraMur := createMasterUserRecord(awaitility, ctx, "extrajohn")
	t.Log("extra MasterUserRecord created at start")
	targetCluster := extraMur.Spec.UserAccounts[0].TargetCluster
	mur := murtest.NewMasterUserRecord("johnsmith",
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(targetCluster))

	// when
	err := awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(ctx))

	// then
	require.NoError(t, err)
	t.Logf("MasterUserRecord '%s' created", mur.Name)

	verifyResourcesExpectingToBeProvisioned(awaitility, mur)
	verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)

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
			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
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
			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
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
			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
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
			verifyResourcesExpectingToBeProvisioned(awaitility, mur)
			verifyResourcesExpectingToBeProvisioned(awaitility, extraMur)
		})
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
		require.NoError(t, err)

		extraMur = wait.NewHostAwaitility(awaitility).GetMasterUserRecord(extraMur.Name)
		verifyResources(awaitility, extraMur, nil, expectingUaConditions(toBeProvisioned()))
	})
}

func TestUserSignupToNamespaceProvisioningFlow(t *testing.T) {

	// ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	// defer ctx.Cleanup()
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	memberCluster, ok, err := awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(t, err)
	require.True(t, ok, "KubeFedCluster should exist")

	t.Run("create namespaces upon signup approval", func(t *testing.T) {
		// 0. Verify that the `basic` NSTemplateTier resource exists (will be needed later)
		basicTier := v1alpha1.NSTemplateTier{}
		err := awaitility.Host().Client.Get(context.TODO(), types.NamespacedName{
			Namespace: awaitility.HostNs,
			Name:      "basic",
		}, &basicTier)
		require.NoError(t, err)
		revisions := make(map[string]string, 3)
		for _, typ := range []string{"code", "dev", "stage"} {
			r, found := namespaceRevision(basicTier, typ)
			require.True(t, found, "unable to find revision for '%s' namespace in the 'basic' NSTemplateTier", typ)
			revisions[typ] = r
		}

		// 1. Create a UserSignup resource
		username := "foo-" + uuid.NewV4().String()
		userID := uuid.NewV4().String()
		userSignup, err := newUserSignup(awaitility.Host(), userID, username, username)
		require.NoError(t, err)
		err = awaitility.Host().Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(ctx))
		require.NoError(t, err, "unable to create usersignup resource")
		// at this stage, the usersignup should not be approved nor completed
		userSignup, err = awaitility.Host().WaitForUserSignupStatusConditions(userSignup.Name,
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			})
		require.NoError(t, err)

		// 2. approve the UserSignup
		userSignup.Spec.Approved = true
		err = awaitility.Host().Client.Update(context.TODO(), userSignup)
		require.NoError(t, err)
		// Check the updated conditions
		_, err = awaitility.Host().WaitForUserSignupStatusConditions(userSignup.Name,
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionTrue,
				Reason: "ApprovedByAdmin",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionTrue,
			})
		require.NoError(t, err)

		// 3. Wait for MUR to be created
		mur, err := awaitility.Host().WaitForMurConditions(username,
			wait.UntilHasStatusCondition(toBeProvisioned()),
			wait.UntilHasUserAccountStatus(
				v1alpha1.UserAccountStatusEmbedded{
					TargetCluster: memberCluster.Name,
					UserAccountStatus: toolchainv1alpha1.UserAccountStatus{
						Conditions: []toolchainv1alpha1.Condition{
							toBeProvisioned(),
						},
					},
				},
			),
		)
		require.NoError(t, err)
		require.Len(t, mur.Spec.UserAccounts, 1)

		// 4. Wait for UserAccount to be ready/provisioned with the expect spec
		err = awaitility.Member().WaitForUserAccount(username,
			v1alpha1.UserAccountSpec{
				UserID:   userID,
				Disabled: false,
				NSLimit:  "default",
				NSTemplateSet: toolchainv1alpha1.NSTemplateSetSpec{
					TierName: "basic",
					Namespaces: []toolchainv1alpha1.NSTemplateSetNamespace{
						{
							Type:     "code",
							Revision: revisions["code"],
							Template: "", // must be empty
						},
						{
							Type:     "dev",
							Revision: revisions["dev"],
							Template: "", // must be empty
						},
						{
							Type:     "stage",
							Revision: revisions["stage"],
							Template: "", // must be empty
						},
					},
				},
			},
			toBeProvisioned(),
		)
		require.NoError(t, err)

		// 5. Wait for User/Identity to be created
		err = awaitility.Member().WaitForUserIdentity(username, "rhd:"+userID)
		require.NoError(t, err)

		// 6. Wait for NSTemplateSet to be ready/provisioned
		err = awaitility.Member().WaitForNSTmplSet(username, toBeProvisioned())
		require.NoError(t, err)

		// 7. Wait for Namespaces to be created
		for typ, revision := range revisions {
			err = awaitility.Member().WaitForNamespace(username, typ, revision)
			require.NoError(t, err)
		}
	})
}

func namespaceRevision(tier v1alpha1.NSTemplateTier, typ string) (string, bool) {
	for _, ns := range tier.Spec.Namespaces {
		if ns.Type == typ {
			return ns.Revision, true
		}
	}
	return "", false
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
	require.NoError(awaitility.T, err)

	murUserAccount := mur.Spec.UserAccounts[0]
	err = memberAwait.WaitForUserAccount(mur.Name, murUserAccount.Spec, expectingUaCons()...)
	require.NoError(awaitility.T, err)

	userAccount := memberAwait.GetUserAccount(mur.Name)
	uaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		TargetCluster:     murUserAccount.TargetCluster,
		UserAccountStatus: userAccount.Status,
	}

	if expectingMurConds != nil {
		_, err = hostAwait.WaitForMurConditions(mur.Name,
			wait.UntilHasUserAccountStatus(uaStatus),
			wait.UntilHasStatusCondition(expectingMurConds()...))
	} else {
		_, err = hostAwait.WaitForMurConditions(mur.Name,
			wait.UntilHasUserAccountStatus(uaStatus))
	}
	require.NoError(awaitility.T, err)

	err = awaitility.Member().WaitForUserIdentity(userAccount.Name, toIdentityName(userAccount.Spec.UserID))
	require.NoError(awaitility.T, err)

	verifyNSTmplSet(memberAwait, userAccount)
}

func verifyNSTmplSet(awaitility *wait.MemberAwaitility, userAcc *toolchainv1alpha1.UserAccount) {
	err := awaitility.WaitForNSTmplSet(userAcc.Name)
	require.NoError(awaitility.T, err)
}

func coolStatus() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionType("CoolType"),
		Status:  corev1.ConditionTrue,
		Reason:  "EverythingIsGood",
		Message: "because our SaaS is cool",
	}
}

func toBeProvisioned() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Provisioned",
	}
}

func createMasterUserRecord(awaitility *wait.Awaitility, ctx *framework.TestCtx, name string) *toolchainv1alpha1.MasterUserRecord {
	memberCluster, ok, err := awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(awaitility.T, err)
	require.True(awaitility.T, ok, "KubeFedCluster should exist")
	mur := murtest.NewMasterUserRecord(name,
		murtest.MetaNamespace(awaitility.HostNs), murtest.TargetCluster(memberCluster.Name))

	err = awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(ctx))
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
	require.NoError(awaitility.T, err, "MasterUserRecord is not deleted")

	err = memberAwait.WaitForDeletedUserAccount(mur.Name)
	require.NoError(awaitility.T, err, "UserAccount is not deleted")

	err = memberAwait.WaitForDeletedUser(mur.Name)
	require.NoError(awaitility.T, err, "User is not deleted")

	err = memberAwait.WaitForDeletedIdentity(mur.Name)
	require.NoError(awaitility.T, err, "Identity is not deleted")
}
