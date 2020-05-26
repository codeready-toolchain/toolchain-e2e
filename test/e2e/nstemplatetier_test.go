package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/require"
)

var toBeComplete = toolchainv1alpha1.Condition{
	Type:   toolchainv1alpha1.ChangeTierRequestComplete,
	Status: corev1.ConditionTrue,
	Reason: toolchainv1alpha1.ChangeTierRequestChangedReason,
}

func TestNSTemplateTiers(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	hostAwaitility := NewHostAwaitility(awaitility)

	// Create and approve "johnsmith" and "extrajohn" signups
	testingTiersName := "testingtiers"
	johnSignup := createAndApproveSignup(t, awaitility, testingTiersName)

	// all tiers to check - keep the basic as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "team", "basic"}

	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	err := awaitility.Client.List(context.TODO(), allTiers, client.InNamespace(awaitility.HostNs))
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, len(tiersToCheck))

	for _, tier := range allTiers.Items {
		assert.Contains(t, tiersToCheck, tier.Name)
	}
	var changeTierRequestNames []string

	// wait for the user to be provisioned for the first time
	verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
	for _, tierToCheck := range tiersToCheck {

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for basic tier)
		_, err := hostAwaitility.WaitForNSTemplateTier(tierToCheck,
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-code-000000a"))),
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-dev-000000a"))),
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-stage-000000a"))),
			UntilNSTemplateTierSpec(Not(HasClusterResourcesTemplateRef("basic-clusterresources-000000a"))),
		)
		require.NoError(t, err)

		t.Run(fmt.Sprintf("promote to %s tier", tierToCheck), func(t *testing.T) {
			// given
			changeTierRequest := newChangeTierRequest(hostAwaitility.Ns, tierToCheck, testingTiersName)

			// when
			err = awaitility.Client.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})

			// then
			require.NoError(t, err)
			_, err := hostAwaitility.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, tierToCheck)
			changeTierRequestNames = append(changeTierRequestNames, changeTierRequest.Name)
		})
	}

	// then - wait until all ChangeTierRequests are deleted by our automatic GC
	for _, name := range changeTierRequestNames {
		err := hostAwaitility.WaitUntilChangeTierRequestDeleted(name)
		assert.NoError(t, err)
	}
}

func TestTierTemplates(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.TierTemplateList{}
	err := awaitility.Client.List(context.TODO(), allTiers, client.InNamespace(awaitility.HostNs))
	// verify that we have 11 tier templates (4+4+3)
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, 11)
}

func TestUpdateOfNamespacesWithLegacyLabels(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	for _, nsType := range []string{"code", "dev", "stage"} {
		err := awaitility.Client.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "legacy-" + nsType,
				Labels: map[string]string{
					"toolchain.dev.openshift.com/provider": "codeready-toolchain",
					"toolchain.dev.openshift.com/owner":    "legacy",
					"toolchain.dev.openshift.com/tier":     "basic",
					"toolchain.dev.openshift.com/type":     nsType,
				},
			},
		}, &test.CleanupOptions{})
		require.NoError(t, err)
	}

	// when
	legacySignup := createAndApproveSignup(t, awaitility, "legacy")

	// then
	verifyResourcesProvisionedForSignup(t, awaitility, legacySignup, "basic")
}

func newChangeTierRequest(namespace, tier, murName string) *toolchainv1alpha1.ChangeTierRequest {
	return &toolchainv1alpha1.ChangeTierRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "changetierrequest-",
		},
		Spec: toolchainv1alpha1.ChangeTierRequestSpec{
			TierName: tier,
			MurName:  murName,
		},
	}
}
