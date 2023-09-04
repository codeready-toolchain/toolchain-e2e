package parallel

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
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
	sa, err := member.WaitForServiceAccount(t, nsName, "namespace-manager")
	require.NoError(t, err)
	expSecretRefs := sa.Secrets
	expPullSecretRefs := sa.ImagePullSecrets

	for i := 0; i < 10; i++ {
		dummySecretRefName := fmt.Sprintf("dummy-secret-%d", i)
		dummyPullSecretRefName := fmt.Sprintf("dummy-pull-secret-%d", i)

		// update the ServiceAccount values so we can verify that some parts will stay and some will be reverted to the needed values
		_, err := member.UpdateServiceAccount(t, nsName, "namespace-manager", func(sa *corev1.ServiceAccount) {

			// when we add an annotation to the SA resource then it should stay there
			sa.Annotations = map[string]string{
				"should": "stay",
			}
			// add secret and ImagePullSecret refs and expect that they will stay there.
			// the actual secrets don't exist, but that's fine - we need to check only if the refs stay in the SA resource
			sa.Secrets = append(sa.Secrets, corev1.ObjectReference{
				Name: dummySecretRefName,
			})
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
				Name: dummyPullSecretRefName,
			})

			// also delete the space label key as we expect that this change should be reverted by the next reconcile loop
			delete(sa.Labels, v1alpha1.SpaceLabelKey)
		})
		require.NoError(t, err)

		// add the generated secret refs to the expected ones
		expSecretRefs = append(expSecretRefs, corev1.ObjectReference{
			Name: dummySecretRefName,
		})
		expPullSecretRefs = append(expPullSecretRefs, corev1.LocalObjectReference{
			Name: dummyPullSecretRefName,
		})

		// Update the namespace annotations & labels to trigger the reconciliation
		_, err = member.UpdateNamespace(t, nsName, func(ns *corev1.Namespace) {
			// drop the last-applied-space-roles annotation, so we are sure that the content of the roles are re-applied
			delete(ns.Annotations, v1alpha1.LastAppliedSpaceRolesAnnotationKey)
			// change the tier name, so we are sure that the content of the tier template is re-applied
			ns.Labels[v1alpha1.TierLabelKey] = "appstudio"
		})
		require.NoError(t, err)

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, mur.Name)
		sa, err := member.WaitForServiceAccount(t, nsName, "namespace-manager")
		require.NoError(t, err)
		assert.Equal(t, "stay", sa.Annotations["should"])

		// verify that the expected secret refs are present
		assert.NotEmpty(t, sa.Secrets)
		for _, expSecret := range expSecretRefs {
			secretFound := false
			for _, secretRef := range sa.Secrets {
				if secretRef.Name == expSecret.Name {
					secretFound = true
					break
				}
			}
			if !secretFound {
				assert.Fail(t, fmt.Sprintf("secret '%s' not found", expSecret.Name))
			}
		}

		// verify that the expected pull secret refs are present
		assert.NotEmpty(t, sa.ImagePullSecrets)
		for _, expPullSecret := range expPullSecretRefs {
			pullSecretFound := false
			for _, pullSecretRef := range sa.ImagePullSecrets {
				if pullSecretRef.Name == expPullSecret.Name {
					pullSecretFound = true
					break
				}
			}
			if !pullSecretFound {
				assert.Fail(t, fmt.Sprintf("pull secret '%s' not found", expPullSecret.Name))
			}
		}

		// verify that the secrets created for SA is the same
		assert.Equal(t, expectedSecrets, getSASecrets(t, member, nsName, sa.Name))
	}

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
