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
		TargetCluster(member).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).
		Resources()

	// and move the user to appstudio tier
	tiers.MoveSpaceToTier(t, awaitilities.Host(), mur.Name, "appstudio-env")
	VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
	nsName := fmt.Sprintf("%s-env", mur.Name)

	expectedSecrets := getSASecrets(t, member, nsName, "namespace-manager")

	for i := 0; i < 10; i++ {
		_, err := member.UpdateServiceAccount(t, nsName, "namespace-manager", func(sa *corev1.ServiceAccount) {

			// when we add an annotation to the SA resource then it should stay there
			sa.Annotations = map[string]string{
				"should": "stay",
			}
			sa.Secrets = append(sa.Secrets, corev1.ObjectReference{
				Name: fmt.Sprintf("dummy-secret-%d", i),
			})
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
				Name: fmt.Sprintf("dummy-pull-secret-%d", i),
			})

		})
		require.NoError(t, err)

		// drop the SpaceRoles annotation from the namespace to trigger the reconciliation
		_, err = member.UpdateNSTemplateSet(t, mur.Name, func(nsTmplSet *v1alpha1.NSTemplateSet) {
			delete(nsTmplSet.Annotations, v1alpha1.LastAppliedSpaceRolesAnnotationKey)
		})
		require.NoError(t, err)

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
		sa, err := member.WaitForServiceAccount(t, nsName, "namespace-manager")
		require.NoError(t, err)
		assert.Equal(t, "stay", sa.Annotations["should"])

		for j := 0; j <= i; j++ {
			expSecretName := fmt.Sprintf("dummy-secret-%d", j)
			secretFound := false
			for _, secretRef := range sa.Secrets {
				if secretRef.Name == expSecretName {
					secretFound = true
					break
				}
			}
			if !secretFound {
				assert.Fail(t, fmt.Sprintf("secret '%s' not found", expSecretName))
			}

			expPullSecretName := fmt.Sprintf("dummy-pull-secret-%d", j)
			pullSecretFound := false
			for _, pullSecretRef := range sa.ImagePullSecrets {
				if pullSecretRef.Name == expPullSecretName {
					pullSecretFound = true
					break
				}
			}
			if !pullSecretFound {
				assert.Fail(t, fmt.Sprintf("pull secret '%s' not found", expPullSecretName))
			}
		}

		// verify that the secrets created for SA is the same
		assert.Equal(t, expectedSecrets, getSASecrets(t, member, nsName, sa.Name))
	}

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
