package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	murtest "github.com/codeready-toolchain/toolchain-common/pkg/test/masteruserrecord"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	uuid "github.com/satori/go.uuid"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()

	johnSignup, johnMur, err := setup(ctx, awaitility, "johnsmith")
	require.NoError(t, err)
	_, extrajohnMur, err := setup(ctx, awaitility, "extrajohn")
	require.NoError(t, err)

	t.Run("try to break UserAccount", func(t *testing.T) {

		t.Run("delete user and wait until is recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnMur.Name}, user)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, johnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extrajohnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete identity and wait until is recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(johnMur.Spec.UserID)}, identity)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, johnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extrajohnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete user mapping and wait until is recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnMur.Name}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = awaitility.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, johnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extrajohnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})

		t.Run("delete identity mapping and wait until is recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(johnMur.Spec.UserID)}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = awaitility.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResources(t, awaitility, johnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
			verifyResources(t, awaitility, extrajohnMur.Name,
				wait.UntilMasterUserRecordHasConditions(isProvisioned()),
				wait.UntilUserAccountHasConditions(isProvisioned()))
		})
	})

	t.Run("delete UserSignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		hostAwait := wait.NewHostAwaitility(awaitility)
		memberAwait := wait.NewMemberAwaitility(awaitility)
		johnSignup, err = hostAwait.WaitForUserSignup(johnSignup.Name)
		require.NoError(t, err)

		// when
		err = awaitility.Client.Delete(context.TODO(), johnSignup)

		// then
		require.NoError(t, err)
		t.Logf("MasterUserRecord '%s' deleted", johnMur.Name)

		err = hostAwait.WaitUntilMasterUserRecordDeleted(johnMur.Name)
		assert.NoError(t, err, "MasterUserRecord is not deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(johnMur.Name)
		assert.NoError(t, err, "UserAccount is not deleted")

		err = memberAwait.WaitUntilUserDeleted(johnMur.Name)
		assert.NoError(t, err, "User is not deleted")

		err = memberAwait.WaitUntilIdentityDeleted(johnMur.Name)
		assert.NoError(t, err, "Identity is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnMur.Name, "code")
		assert.NoError(t, err, "johnsmith-code namnespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnMur.Name, "dev")
		assert.NoError(t, err, "johnsmith-dev namnespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnMur.Name, "stage")
		assert.NoError(t, err, "johnsmith-stage namnespace is not deleted")

		// also, verify that other user's resource are left intact
		extrajohnMur, err = hostAwait.WaitForMasterUserRecord(extrajohnMur.Name)
		require.NoError(t, err)
		verifyResources(t, awaitility, extrajohnMur.Name, wait.UntilMasterUserRecordHasConditions(isProvisioned()))
	})
}

func setup(ctx *framework.TestCtx, awaitility *wait.Awaitility, username string) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord, error) {
	memberCluster, ok, err := awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("KubeFedCluster should exist")
	}

	// 0. Verify that the `basic` NSTemplateTier resource exists (will be needed later)
	basicTier := v1alpha1.NSTemplateTier{}
	err = awaitility.Host().Client.Get(context.TODO(), types.NamespacedName{
		Namespace: awaitility.HostNs,
		Name:      "basic",
	}, &basicTier)
	if err != nil {
		return nil, nil, err
	}
	revisions := make(map[string]string, 3)
	for _, typ := range []string{"code", "dev", "stage"} {
		if r, found := namespaceRevision(basicTier, typ); found {
			revisions[typ] = r
			continue
		}
		return nil, nil, fmt.Errorf("unable to find revision for '%s' namespace in the 'basic' NSTemplateTier", typ)
	}

	// 1. Create a UserSignup resource
	userID := uuid.NewV4().String()
	userSignup, err := newUserSignup(awaitility.Host(), userID, username, username)
	if err != nil {
		return nil, nil, err
	}
	err = awaitility.Host().Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(ctx))
	if err != nil {
		return nil, nil, err
	}
	// at this stage, the usersignup should not be approved nor completed
	userSignup, err = awaitility.Host().WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			}))
	if err != nil {
		return nil, nil, err
	}

	// 2. approve the UserSignup
	userSignup.Spec.Approved = true
	err = awaitility.Host().Client.Update(context.TODO(), userSignup)
	if err != nil {
		return nil, nil, err
	}
	// Check the updated conditions
	_, err = awaitility.Host().WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionTrue,
				Reason: "ApprovedByAdmin",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionTrue,
			}))
	if err != nil {
		return nil, nil, err
	}

	// 3. Wait for MUR to be created
	mur, err := awaitility.Host().WaitForMasterUserRecord(username,
		wait.UntilMasterUserRecordHasConditions(isProvisioned()),
		wait.UntilMasterUserRecordHasUserAccountStatuses(
			v1alpha1.UserAccountStatusEmbedded{
				TargetCluster: memberCluster.Name,
				UserAccountStatus: toolchainv1alpha1.UserAccountStatus{
					Conditions: []toolchainv1alpha1.Condition{
						isProvisioned(),
					},
				},
			},
		),
	)
	if err != nil {
		return nil, nil, err
	}
	if len(mur.Spec.UserAccounts) != 1 {
		return nil, nil, fmt.Errorf("unexpected number of user accounts: %d", len(mur.Spec.UserAccounts))
	}

	// 4. Wait for UserAccount to be ready/provisioned with the expect spec
	_, err = awaitility.Member().WaitForUserAccount(username,
		wait.UntilUserAccountHasSpec(
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
			}),
		wait.UntilUserAccountHasConditions(isProvisioned()),
	)
	if err != nil {
		return nil, nil, err
	}

	// 5. Wait for User/Identity to be created
	_, err = awaitility.Member().WaitForUser(username)
	if err != nil {
		return nil, nil, err
	}
	_, err = awaitility.Member().WaitForIdentity("rhd:" + userID)
	if err != nil {
		return nil, nil, err
	}

	// 6. Wait for NSTemplateSet to be ready/provisioned
	_, err = awaitility.Member().WaitForNSTmplSet(username,
		wait.UntilNSTemplateSetHasConditions(isProvisioned()))
	if err != nil {
		return nil, nil, err
	}

	// 7. Wait for Namespaces to be created
	for typ, revision := range revisions {
		if _, err := awaitility.Member().WaitForNamespace(username, typ, revision); err != nil {
			return nil, nil, err
		}
	}
	return userSignup, mur, nil
}

func namespaceRevision(tier v1alpha1.NSTemplateTier, typ string) (string, bool) {
	for _, ns := range tier.Spec.Namespaces {
		if ns.Type == typ {
			return ns.Revision, true
		}
	}
	return "", false
}

func TestE2EFlowForMultipleAccounts(t *testing.T) {
	// given
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, murList)
	defer ctx.Cleanup()

	// when
	var murs []*toolchainv1alpha1.MasterUserRecord
	for i := 0; i < 10; i++ {
		mur := createMasterUserRecord(t, awaitility, ctx, fmt.Sprintf("johny-number-%d", i))
		t.Logf("MasterUserRecord '%s' created", mur.Name)
		murs = append(murs, mur)
	}

	// then
	for _, mur := range murs {
		verifyResources(t, awaitility, mur.Name,
			wait.UntilMasterUserRecordHasConditions(isProvisioned()),
			wait.UntilUserAccountHasConditions(isProvisioned()))
	}
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
