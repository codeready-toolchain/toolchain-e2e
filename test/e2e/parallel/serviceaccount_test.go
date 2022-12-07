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
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	member := awaitilities.Member1()

	// let's provision user
	_, mur := NewSignupRequest(awaitilities).
		Username("do-not-override-sa").
		ManuallyApprove().
		TargetCluster(member.ClusterName).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).
		Resources()

	// and move the user to appstudio tier
	tiers.MoveSpaceToTier(t, awaitilities.Host(), mur.Name, "appstudio")
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)

	// get the SA that is provisioned for the user in the ns
	sa := member.WaitForServiceAccount(t, mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	expectedSecrets := getSASecrets(t, member, mur.Name, sa.Name)

	// when we add an annotation to the SA resource then it should stay there
	sa.Annotations = map[string]string{
		"should": "stay",
	}
	require.NoError(t, member.Client.Update(context.TODO(), sa))

	// drop the SpaceRoles annotation from the namespace to trigger the reconciliation
	member.UpdateNSTemplateSet(t, mur.Name,
		func(nsTmplSet *v1alpha1.NSTemplateSet) {
			delete(nsTmplSet.Annotations, v1alpha1.LastAppliedSpaceRolesAnnotationKey)
		})

	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
	sa = member.WaitForServiceAccount(t, mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	assert.Equal(t, "stay", sa.Annotations["should"])

	// verify that the secrets created for SA is the same
	assert.Equal(t, expectedSecrets, getSASecrets(t, member, mur.Name, sa.Name))

}

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
