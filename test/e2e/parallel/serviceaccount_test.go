package parallel

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDoNotOverrideServiceAccount(t *testing.T) {
	// given
	// Skipping the TestDoNotOverrideServiceAccount test instead of deleting it because we will need to create SAs as part
	// of the environment sub-workspaces so the test & logic will be useful to keep.
	t.Skip("skipping since the user SA was removed as part of https://github.com/codeready-toolchain/host-operator/pull/719. To be added back with https://issues.redhat.com/browse/ASC-249")
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	member := awaitilities.Member1()

	// let's provision user
	_, mur := NewSignupRequest(awaitilities).
		Username("do-not-override-sa").
		ManuallyApprove().
		TargetCluster(member).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).
		Resources()

	// and move the user to appstudio tier
	tiers.MoveSpaceToTier(t, awaitilities.Host(), mur.Name, "appstudio")
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)

	// get the SA that is provisioned for the user in the ns
	sa, err := member.WaitForServiceAccount(t, mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	require.NoError(t, err)
	expectedSecrets := getSASecrets(t, member, mur.Name, sa.Name)

	// when we add an annotation to the SA resource then it should stay there
	sa.Annotations = map[string]string{
		"should": "stay",
	}
	require.NoError(t, member.Client.Update(context.TODO(), sa))

	// drop the SpaceRoles annotation from the namespace to trigger the reconciliation
	require.NoError(t, err)
	_, err = member.UpdateNSTemplateSet(t, mur.Name, func(nsTmplSet *v1alpha1.NSTemplateSet) {
		delete(nsTmplSet.Annotations, v1alpha1.LastAppliedSpaceRolesAnnotationKey)
	})
	require.NoError(t, err)

	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
	sa, err = member.WaitForServiceAccount(t, mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	require.NoError(t, err)
	assert.Equal(t, "stay", sa.Annotations["should"])

	// verify that the secrets created for SA is the same
	assert.Equal(t, expectedSecrets, getSASecrets(t, member, mur.Name, sa.Name))

}

// TODO: remove the nolint:unused once the test is not skipped anymore
// nolint:unused
func getSASecrets(t *testing.T, member *wait.MemberAwaitility, ns, saName string) []string {
	var saSecrets []string
	secrets := &corev1.SecretList{}
	require.NoError(t, member.Client.List(context.TODO(), secrets, client.InNamespace(ns)))
	for _, secret := range secrets.Items {
		// filter only those that are marked to be owned by the SA
		if secret.Annotations[corev1.ServiceAccountNameKey] == saName {
			saSecrets = append(saSecrets, secret.Name)
		}
	}
	sort.Strings(saSecrets)
	return saSecrets
}
