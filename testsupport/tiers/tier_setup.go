package tiers

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/configuration"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TierModifier func(tier *toolchainv1alpha1.NSTemplateTier) error

type CustomNSTemplateTier struct {
	// the "base1ns" NSTemplateTier
	*toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the cluster resources (by default, the name of the NSTemplateTier used to create this custom one)
	ClusterResourcesTier *toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the namespace resources (by default, the name of the NSTemplateTier used to create this custom one)
	NamespaceResourcesTier *toolchainv1alpha1.NSTemplateTier
	// name of the tier used to set the space roles (by default, the name of the NSTemplateTier used to create this custom one)
	SpaceRolesTier *toolchainv1alpha1.NSTemplateTier
}

type CustomNSTemplateTierModifier func(*wait.HostAwaitility, *CustomNSTemplateTier) error

type TierTemplateModifier func(*toolchainv1alpha1.TierTemplate) error

func WithClusterResources(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier, modifiers ...TierTemplateModifier) CustomNSTemplateTierModifier {
	return func(hostAwait *wait.HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.ClusterResourcesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, otherTier.Spec.ClusterResources.TemplateRef, modifiers...)
		if err != nil {
			return err
		}
		tier.Spec.ClusterResources = &toolchainv1alpha1.NSTemplateTierClusterResources{
			TemplateRef: tier.Status.Revisions[tmplRef],
		}
		return nil
	}
}

func WithNamespaceResources(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier, modifiers ...TierTemplateModifier) CustomNSTemplateTierModifier {
	return func(hostAwait *wait.HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.NamespaceResourcesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tier.Spec.Namespaces = make([]toolchainv1alpha1.NSTemplateTierNamespace, len(otherTier.Spec.Namespaces))
		for i, def := range otherTier.Spec.Namespaces {
			tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, def.TemplateRef, modifiers...)
			if err != nil {
				return err
			}
			tier.Spec.Namespaces[i].TemplateRef = tier.Status.Revisions[tmplRef]
		}
		return nil
	}
}

func WithSpaceRoles(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier, modifiers ...TierTemplateModifier) CustomNSTemplateTierModifier {
	return func(hostAwait *wait.HostAwaitility, tier *CustomNSTemplateTier) error {
		tier.SpaceRolesTier = otherTier
		// configure the "wrapped" NSTemplateTier
		tier.Spec.SpaceRoles = make(map[string]toolchainv1alpha1.NSTemplateTierSpaceRole, len(otherTier.Spec.SpaceRoles))
		for name, def := range otherTier.Spec.SpaceRoles {
			tmplRef, err := duplicateTierTemplate(t, hostAwait, otherTier.Namespace, tier.Name, def.TemplateRef, modifiers...)
			if err != nil {
				return err
			}
			tier.Spec.SpaceRoles[name] = toolchainv1alpha1.NSTemplateTierSpaceRole{
				TemplateRef: tier.Status.Revisions[tmplRef],
			}
		}
		return nil
	}
}

func WithParameter(name, value string) CustomNSTemplateTierModifier {
	return func(hostAwait *wait.HostAwaitility, tier *CustomNSTemplateTier) error {
		if tier.Spec.Parameters == nil {
			tier.Spec.Parameters = []toolchainv1alpha1.Parameter{}
		}
		tier.Spec.Parameters = append(tier.Spec.Parameters,
			toolchainv1alpha1.Parameter{
				Name:  name,
				Value: value,
			},
		)
		return nil
	}
}

// CreateCustomNSTemplateTier creates a custom tier.
// If no modifiers provided then the new tier will use copies of the baseTier cluster, namespace and space roles templates
// without any modifications.
func CreateCustomNSTemplateTier(t *testing.T, hostAwait *wait.HostAwaitility, name string, baseTier *toolchainv1alpha1.NSTemplateTier, modifiers ...CustomNSTemplateTierModifier) *CustomNSTemplateTier {
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
			Status: toolchainv1alpha1.NSTemplateTierStatus{
				//default values
			},
		},
	}
	if len(modifiers) == 0 {
		// If no modifiers provided then use default modifiers which would use resources from the base tier.
		modifiers = []CustomNSTemplateTierModifier{
			WithClusterResources(t, baseTier),
			WithNamespaceResources(t, baseTier),
			WithSpaceRoles(t, baseTier),
		}
	}

	// ... and apply
	for _, modify := range modifiers {
		err := modify(hostAwait, tier)
		require.NoError(t, err)
	}
	err := hostAwait.CreateWithCleanup(t, tier.NSTemplateTier)
	require.NoError(t, err)
	return tier
}

// UpdateCustomNSTemplateTier updates the given "tier" using the modifiers
// returns the latest version of the NSTemplateTier
func UpdateCustomNSTemplateTier(t *testing.T, hostAwait *wait.HostAwaitility, tier *CustomNSTemplateTier, modifiers ...CustomNSTemplateTierModifier) *CustomNSTemplateTier {
	// reload the underlying NSTemplateTier resource before modifying it
	tmplTier, err := hostAwait.WaitForNSTemplateTier(t, tier.Name)
	require.NoError(t, err)
	tier.NSTemplateTier = tmplTier
	// make sure we have the very latest version of the given tier (to avoid the update conflict on the server-side)
	for _, modify := range modifiers {
		err := modify(hostAwait, tier)
		require.NoError(t, err)
	}
	_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.NSTemplateTier{}).
		Update(tier.NSTemplateTier.Name, hostAwait.Namespace, func(nstt *toolchainv1alpha1.NSTemplateTier) {
			nstt.Spec = tier.NSTemplateTier.Spec
			nstt.Status.Revisions = tier.Status.Revisions
		})
	require.NoError(t, err)
	return tier
}

func duplicateTierTemplate(t *testing.T, hostAwait *wait.HostAwaitility, namespace, tierName, origTemplateRef string, modifiers ...TierTemplateModifier) (string, error) {
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
	for _, modify := range modifiers {
		err := modify(newTierTemplate)
		require.NoError(t, err)
	}

	newTierTemplate.Spec.TierName = tierName
	if err := hostAwait.CreateWithCleanup(t, newTierTemplate); err != nil {
		if !errors.IsAlreadyExists(err) {
			return "", err
		}
	}
	return newTierTemplate.Name, nil
}

func MoveSpaceToTier(t *testing.T, hostAwait *wait.HostAwaitility, spacename, tierName string) {
	t.Logf("moving space '%s' to space tier '%s'", spacename, tierName)
	_, err := hostAwait.WaitForSpace(t, spacename)
	require.NoError(t, err)

	_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
		Update(spacename, hostAwait.Namespace,
			func(s *toolchainv1alpha1.Space) {
				s.Spec.TierName = tierName
			})
	require.NoError(t, err)
}

func MoveMURToTier(t *testing.T, hostAwait *wait.HostAwaitility, username, tierName string) {
	t.Logf("moving masteruserrecord '%s' to user tier '%s'", username, tierName)
	mur, err := hostAwait.WaitForMasterUserRecord(t, username)
	require.NoError(t, err)

	_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
		Update(mur.Name, hostAwait.Namespace,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Spec.TierName = tierName
			})
	require.NoError(t, err)
}

func GetDefaultSpaceTierName(t *testing.T, hostAwait *wait.HostAwaitility) string {
	toolchainConfig := hostAwait.GetToolchainConfig(t)
	return configuration.GetString(toolchainConfig.Spec.Host.Tiers.DefaultSpaceTier, "base")
}
