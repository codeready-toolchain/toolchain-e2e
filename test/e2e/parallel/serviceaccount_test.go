package parallel

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
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
	_, mur := NewSignupRequest(t, awaitilities).
		Username("do-not-override-sa").
		ManuallyApprove().
		TargetCluster(member).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().
		Resources()

	// and move the user to appstudio tier
	tiers.MoveSpaceToTier(t, awaitilities.Host(), mur.Name, "appstudio")
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)

	// get the SA that is provisioned for the user in the ns
	sa, err := member.WaitForServiceAccount(mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	require.NoError(t, err)

	// when we add an annotation to the SA resource then it should stay there
	sa.Annotations = map[string]string{
		"should": "stay",
	}
	require.NoError(t, member.Client.Update(context.TODO(), sa))

	// drop the SpaceRoles annotation from the namespace to trigger the reconciliation
	require.NoError(t, err)
	_, err = member.UpdateNSTemplateSet(mur.Name, func(nsTmplSet *v1alpha1.NSTemplateSet) {
		delete(nsTmplSet.Annotations, v1alpha1.LastAppliedSpaceRolesAnnotationKey)
	})
	require.NoError(t, err)

	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
	sa, err = member.WaitForServiceAccount(mur.Name, fmt.Sprintf("appstudio-%s", mur.Name))
	require.NoError(t, err)
	assert.Equal(t, "stay", sa.Annotations["should"])

	// verify the secrets created for SA
	// list all secrets in the namespace
	secrets := &corev1.SecretList{}
	require.NoError(t, member.Client.List(context.TODO(), secrets, client.InNamespace(mur.Name)))
	for _, secret := range secrets.Items {
		// filter only those that are marked to be owned by the SA
		if secret.Annotations[corev1.ServiceAccountNameKey] == sa.Name {
			found := false
			// check if the secret is listed in the SA's secrets
			for _, saSecret := range sa.Secrets {
				if saSecret.Name == secret.Name {
					found = true
					break
				}
			}
			assert.True(t, found, "the secret %s is marked to be owned by SA % but it wasn't listed in the secrets of the SA: %v", secret.Name, sa.Namespace, sa.Secrets)
		}
	}
}
