package spaceprovisionerconfig

import (
	toolchainv1aplha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	assertions "github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions2"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions2/conditions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions2/metadata"
	"github.com/stretchr/testify/assert"
)

type Assertions struct {
	Metadata   metadata.Assertions[Assertions, *toolchainv1aplha1.SpaceProvisionerConfig]
	Conditions conditions.Assertions[Assertions, *toolchainv1aplha1.SpaceProvisionerConfig]
	assertions.EmbeddableObjectAssertions[Assertions, *toolchainv1aplha1.SpaceProvisionerConfig]
}

func Asserting() *Assertions {
	assertions := []assertions.Assertion[*toolchainv1aplha1.SpaceProvisionerConfig]{}
	instance := &Assertions{}
	instance.Conditions.WireUp(instance, &assertions, func(spc *toolchainv1aplha1.SpaceProvisionerConfig) *[]toolchainv1aplha1.Condition {
		return &spc.Status.Conditions
	})
	instance.Metadata.WireUp(instance, &assertions)
	instance.WireUp(instance, &assertions)

	return instance
}

func (a *Assertions) ReferencesToolchainCluster(tc string) *Assertions {
	a.AddAssertionFunc(func(t assertions.AssertT, spc *toolchainv1aplha1.SpaceProvisionerConfig) {
		assert.Equal(t, tc, spc.Spec.ToolchainCluster)
	})
	return a
}

var _ assertions.WithAssertions[*toolchainv1aplha1.SpaceProvisionerConfig] = (*Assertions)(nil)
