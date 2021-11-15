package e2e_test

import (
	"context"
	"testing"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/davecgh/go-spew/spew"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMigrateNSTemplateSet(t *testing.T) {

	awaitilities := WaitForDeployments(t)
	memberAwait := awaitilities.Member1()

	// CRT-1321: remove OwnerReferences in NSTemplateSet
	// This test shows that OwnerReference can be reset in NSTemplateSet
	t.Run("remove owner refs", func(t *testing.T) {
		// given
		userSignup, _ := NewSignupRequest(t, awaitilities).
			Username("nstmplsetuser").
			Email("promoteuser@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "base")
		// check the NSTemplateSet
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(userSignup.Spec.Username, wait.UntilNSTemplateSetHasNoOwnerReferences())
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)

		// update the nsTmplSet by setting an OwnerReference
		userAccount, err := memberAwait.WaitForUserAccount(userSignup.Spec.Username)
		require.NoError(t, err)
		require.NotEmpty(t, userAccount)
		yes := true
		ownerRef := metav1.OwnerReference{
			APIVersion:         "toolchain.dev.openshift.com/v1alpha1",
			Kind:               "UserAccount",
			Name:               userAccount.Name,
			UID:                userAccount.UID,
			Controller:         &yes,
			BlockOwnerDeletion: &yes,
		}
		t.Logf(spew.Sdump(ownerRef))
		updatedNSTmplSet := nsTmplSet.DeepCopy()
		updatedNSTmplSet.OwnerReferences = []metav1.OwnerReference{
			ownerRef,
		}
		t.Logf("pre-updated ResourceVersion: %s", updatedNSTmplSet.ResourceVersion)
		err = memberAwait.Client.Update(context.TODO(), nsTmplSet)
		require.NoError(t, err)
		t.Logf("post-updated ResourceVersion: %s", nsTmplSet.ResourceVersion)
		assert.Greater(t, updatedNSTmplSet.ResourceVersion, nsTmplSet.ResourceVersion) // verify that the updated happened, ie, the ResourceVersion was incremented
		// check that the update occurred
		reconciledNSTmplSet, err := memberAwait.WaitForNSTmplSet(userSignup.Spec.Username, wait.UntilNSTemplateSetHasNoOwnerReferences())
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)
		assert.Greater(t, reconciledNSTmplSet.ResourceVersion, nsTmplSet.ResourceVersion) // verify that a reconcile loop occurred, ie, the ResourceVersion was incremented once more
	})

}
