package testsupport

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stretchr/testify/require"
)

type TierModifier func(tier *toolchainv1alpha1.NSTemplateTier) error

var toBeComplete = toolchainv1alpha1.Condition{
	Type:   toolchainv1alpha1.ChangeTierRequestComplete,
	Status: corev1.ConditionTrue,
	Reason: toolchainv1alpha1.ChangeTierRequestChangedReason,
}

func CreateNSTemplateTier(t *testing.T, ctx *test.Context, hostAwait *HostAwaitility, tierName string, modifiers ...TierModifier) *toolchainv1alpha1.NSTemplateTier {
	// We'll use the `basic` tier as a source of inspiration.
	WaitUntilBasicNSTemplateTierIsUpdated(t, hostAwait)
	basicTier := &toolchainv1alpha1.NSTemplateTier{}
	err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwait.Namespace,
		Name:      "basic",
	}, basicTier)
	require.NoError(t, err)

	// now let's create the new NSTemplateTier with the same templates as the "basic" tier
	tier := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: basicTier.Namespace,
			Name:      tierName,
		},
		Spec: basicTier.Spec,
	}

	err = Modify(tier, modifiers...)
	require.NoError(t, err)

	err = hostAwait.FrameworkClient.Create(context.TODO(), tier, CleanupOptions(ctx))
	require.NoError(t, err)

	return tier
}

func MoveUserToTier(t *testing.T, hostAwait *HostAwaitility, username string, tier toolchainv1alpha1.NSTemplateTier) *toolchainv1alpha1.MasterUserRecord {
	mur, err := hostAwait.WaitForMasterUserRecord(username,
		UntilMasterUserRecordHasCondition(Provisioned())) // ignore other conditions, such as notification sent, etc.
	require.NoError(t, err)
	changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, username, tier.Name)
	err = hostAwait.FrameworkClient.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})
	require.NoError(t, err)
	_, err = hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
	require.NoError(t, err)
	return mur
}

func NewChangeTierRequest(namespace, murName, tier string) *toolchainv1alpha1.ChangeTierRequest {
	return &toolchainv1alpha1.ChangeTierRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "changetierrequest-",
		},
		Spec: toolchainv1alpha1.ChangeTierRequestSpec{
			MurName:  murName,
			TierName: tier,
		},
	}
}

func Modify(tier *toolchainv1alpha1.NSTemplateTier, modifiers ...TierModifier) error {
	for _, modify := range modifiers {
		if err := modify(tier); err != nil {
			return err
		}
	}
	return nil
}

func DeactivationTimeoutDays(timeoutDurationDays int) TierModifier {
	return func(tier *toolchainv1alpha1.NSTemplateTier) error {
		tier.Spec.DeactivationTimeoutDays = timeoutDurationDays
		return nil
	}
}

func WaitUntilBasicNSTemplateTierIsUpdated(t *testing.T, hostAwait *HostAwaitility) {
	_, err := hostAwait.WaitForNSTemplateTier("basic",
		UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
	require.NoError(t, err)
}
