package tiers

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait" // nolint:revive

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

type TierModifier func(tier *toolchainv1alpha1.NSTemplateTier) error

type CustomNSTemplateTier struct {
	// the "base" NSTemplateTier
	*toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the cluster resources (by default, the name of the NSTemplateTier used to create this custom one)
	ClusterResourcesTier *toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the namespace resources (by default, the name of the NSTemplateTier used to create this custom one)
	NamespaceResourcesTier *toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the space roles (by default, the name of the NSTemplateTier used to create this custom one)
	SpaceRolesTier *toolchainv1alpha1.NSTemplateTier
}

type CustomNSTemplateTierModifier func(*HostAwaitility, *CustomNSTemplateTier) error

func WithClusterResources(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier) CustomNSTemplateTierModifier {
	return func(hostAwait *HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.ClusterResourcesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, otherTier.Spec.ClusterResources.TemplateRef)
		if err != nil {
			return err
		}
		tier.Spec.ClusterResources = &toolchainv1alpha1.NSTemplateTierClusterResources{
			TemplateRef: tmplRef,
		}
		return nil
	}
}

func WithNamespaceResources(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier) CustomNSTemplateTierModifier {
	return func(hostAwait *HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.NamespaceResourcesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tier.Spec.Namespaces = make([]toolchainv1alpha1.NSTemplateTierNamespace, len(otherTier.Spec.Namespaces))
		for i, def := range otherTier.Spec.Namespaces {
			tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, def.TemplateRef)
			if err != nil {
				return err
			}
			tier.Spec.Namespaces[i].TemplateRef = tmplRef
		}
		return nil
	}
}

func WithSpaceRoles(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier) CustomNSTemplateTierModifier {
	return func(hostAwait *HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.SpaceRolesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tier.Spec.SpaceRoles = make(map[string]toolchainv1alpha1.NSTemplateTierSpaceRole, len(otherTier.Spec.SpaceRoles))
		for name, def := range otherTier.Spec.SpaceRoles {
			tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, def.TemplateRef)
			if err != nil {
				return err
			}
			tier.Spec.SpaceRoles[name] = toolchainv1alpha1.NSTemplateTierSpaceRole{
				TemplateRef: tmplRef,
			}
		}
		return nil
	}
}

func CreateCustomNSTemplateTier(t *testing.T, hostAwait *HostAwaitility, name string, baseTier *toolchainv1alpha1.NSTemplateTier, modifiers ...CustomNSTemplateTierModifier) *CustomNSTemplateTier {
	tier := &CustomNSTemplateTier{
		NSTemplateTier: &toolchainv1alpha1.NSTemplateTier{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: baseTier.Namespace,
				Name:      name,
				Labels:    map[string]string{"producer": "toolchain-e2e"},
			},
			Spec: toolchainv1alpha1.NSTemplateTierSpec{
				// default values
			},
		},
	}
	// add default values before custom values...
	modifiers = append([]CustomNSTemplateTierModifier{
		WithClusterResources(t, baseTier),
		WithNamespaceResources(t, baseTier),
		WithSpaceRoles(t, baseTier),
	}, modifiers...)

	// ... and apply
	for _, modify := range modifiers {
		err := modify(hostAwait, tier)
		require.NoError(t, err)
	}
	hostAwait.CreateWithCleanup(t, tier.NSTemplateTier)
	return tier
}

// createCustomNSTemplateTier updates the given "tier" using the modifiers
// returns the latest version of the NSTemplateTier
func UpdateCustomNSTemplateTier(t *testing.T, hostAwait *HostAwaitility, tier *CustomNSTemplateTier, modifiers ...CustomNSTemplateTierModifier) *CustomNSTemplateTier {
	// reload the underlying NSTemplateTier resource before modifying it
	tmplTier := hostAwait.WaitForNSTemplateTier(t, tier.Name)
	tier.NSTemplateTier = tmplTier
	// make sure we have the very latest version of the given tier (to avoid the update conflict on the server-side)
	for _, modify := range modifiers {
		err := modify(hostAwait, tier)
		require.NoError(t, err)
	}
	err := hostAwait.Client.Update(context.TODO(), tier.NSTemplateTier)
	require.NoError(t, err)
	return tier
}

func duplicateTierTemplate(t *testing.T, hostAwait *HostAwaitility, namespace, tierName, origTemplateRef string) (string, error) {
	origTierTemplate := &toolchainv1alpha1.TierTemplate{}
	if err := hostAwait.Client.Get(context.TODO(), test.NamespacedName(hostAwait.Namespace, origTemplateRef), origTierTemplate); err != nil {
		return "", err
	}
	newTierTemplate := &toolchainv1alpha1.TierTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      fmt.Sprintf("%sfrom%s", tierName, origTierTemplate.Name),
			Labels:    map[string]string{"producer": "toolchain-e2e"},
		},
		Spec: origTierTemplate.Spec,
	}
	newTierTemplate.Spec.TierName = tierName
	hostAwait.CreateWithCleanup(t, newTierTemplate)
	return newTierTemplate.Name, nil
}

func MoveSpaceToTier(t *testing.T, hostAwait *HostAwaitility, spacename, tierName string) {
	t.Logf("moving space '%s' to space tier '%s'", spacename, tierName)
	hostAwait.WaitForSpace(t, spacename)

	hostAwait.UpdateSpace(t, spacename,
		func(s *toolchainv1alpha1.Space) {
			s.Spec.TierName = tierName
		})
}

func MoveMURToTier(t *testing.T, hostAwait *HostAwaitility, username, tierName string) {
	t.Logf("moving masteruserrecord '%s' to user tier '%s'", username, tierName)
	mur := hostAwait.WaitForMasterUserRecord(t, username)

	hostAwait.UpdateMasterUserRecord(t, false, mur.Name,
		func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.TierName = tierName
		})
}
