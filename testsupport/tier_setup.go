package testsupport

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait" // nolint:revive
	"k8s.io/apimachinery/pkg/api/errors"

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

func CreateNSTemplateTier(t *testing.T, hostAwait *HostAwaitility, tierName string, modifiers ...TierModifier) *toolchainv1alpha1.NSTemplateTier {
	// We'll use the `base` tier as a source of inspiration.
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)
	baseTier := &toolchainv1alpha1.NSTemplateTier{}
	err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwait.Namespace,
		Name:      "base",
	}, baseTier)
	require.NoError(t, err)

	tier := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseTier.Namespace,
			Name:      tierName,
		},
		Spec: toolchainv1alpha1.NSTemplateTierSpec{
			DeactivationTimeoutDays: baseTier.Spec.DeactivationTimeoutDays,
		},
	}

	SetNamespaceTierTemplatesFromTier(t, hostAwait, tier, baseTier)

	SetClusterTierTemplateFromTier(t, hostAwait, tier, baseTier)

	err = Modify(tier, modifiers...)
	require.NoError(t, err)

	err = hostAwait.CreateWithCleanup(context.TODO(), tier)
	require.NoError(t, err)

	return tier
}

func SetNamespaceTierTemplatesFromTier(t *testing.T, hostAwait *HostAwaitility, targetTier *toolchainv1alpha1.NSTemplateTier, sourceTier *toolchainv1alpha1.NSTemplateTier) {
	targetTier.Spec.Namespaces = []toolchainv1alpha1.NSTemplateTierNamespace{}
	for _, ns := range sourceTier.Spec.Namespaces {
		targetTier.Spec.Namespaces = append(targetTier.Spec.Namespaces, toolchainv1alpha1.NSTemplateTierNamespace{
			TemplateRef: createNewTierTemplate(t, hostAwait, targetTier.Name, ns.TemplateRef, sourceTier.Namespace),
		})
	}
}

func SetClusterTierTemplateFromTier(t *testing.T, hostAwait *HostAwaitility, targetTier *toolchainv1alpha1.NSTemplateTier, sourceTier *toolchainv1alpha1.NSTemplateTier) {
	if sourceTier.Spec.ClusterResources != nil {
		targetTier.Spec.ClusterResources = &toolchainv1alpha1.NSTemplateTierClusterResources{
			TemplateRef: createNewTierTemplate(t, hostAwait, targetTier.Name, sourceTier.Spec.ClusterResources.TemplateRef, sourceTier.Namespace),
		}
	} else {
		targetTier.Spec.ClusterResources = nil
	}
}

func createNewTierTemplate(t *testing.T, hostAwait *HostAwaitility, tierName, origTemplateRef, namespace string) string {
	origTierTemplate := &toolchainv1alpha1.TierTemplate{}
	err := hostAwait.Client.Get(context.TODO(), test.NamespacedName(hostAwait.Namespace, origTemplateRef), origTierTemplate)
	require.NoError(t, err)
	newTierTemplate := &toolchainv1alpha1.TierTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      fmt.Sprintf("%sfrom%s", tierName, origTierTemplate.Name),
			Labels:    map[string]string{"producer": "toolchain-e2e"},
		},
		Spec: origTierTemplate.Spec,
	}
	newTierTemplate.Spec.TierName = tierName
	if err := hostAwait.CreateWithCleanup(context.TODO(), newTierTemplate); err != nil {
		if !errors.IsAlreadyExists(err) {
			require.NoError(t, err)
		}
	}
	return newTierTemplate.Name
}

func MoveUserToTier(t *testing.T, hostAwait *HostAwaitility, username, tierName string) {
	changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, username, tierName)
	err := hostAwait.CreateWithCleanup(context.TODO(), changeTierRequest)
	require.NoError(t, err)
	_, err = hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
	require.NoError(t, err)
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

func WaitUntilBaseNSTemplateTierIsUpdated(t *testing.T, hostAwait *HostAwaitility) {
	_, err := hostAwait.WaitForNSTemplateTier("base",
		UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
	require.NoError(t, err)
}
