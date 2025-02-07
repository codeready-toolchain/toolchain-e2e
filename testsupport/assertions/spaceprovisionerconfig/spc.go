package spaceprovisionerconfig

import (
	toolchainv1aplha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/conditions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/object"
	"github.com/stretchr/testify/assert"
)

type (
	ObjectAssertions    = object.Assertions[Assertions, *toolchainv1aplha1.SpaceProvisionerConfig]
	ConditionAssertions = conditions.Assertions[Assertions, *toolchainv1aplha1.SpaceProvisionerConfig]

	Assertions struct {
		ObjectAssertions
		ConditionAssertions
		assertions []assertions.Assertion[*toolchainv1aplha1.SpaceProvisionerConfig]
	}
)

func (a *Assertions) Assertions() []assertions.Assertion[*toolchainv1aplha1.SpaceProvisionerConfig] {
	return a.assertions
}

func That() *Assertions {
	instance := &Assertions{assertions: []assertions.Assertion[*toolchainv1aplha1.SpaceProvisionerConfig]{}}
	instance.EmbedInto(instance, &instance.assertions, func(spc *toolchainv1aplha1.SpaceProvisionerConfig) *[]toolchainv1aplha1.Condition {
		return &spc.Status.Conditions
	})
	instance.ObjectAssertions.EmbedInto(instance, &instance.assertions)
	return instance
}

func (a *Assertions) ReferencesToolchainCluster(tc string) *Assertions {
	a.assertions = append(a.assertions, &assertions.AssertAndFixFunc[*toolchainv1aplha1.SpaceProvisionerConfig]{
		Assert: func(t assertions.AssertT, spc *toolchainv1aplha1.SpaceProvisionerConfig) {
			assert.Equal(t, tc, spc.Spec.ToolchainCluster)
		},
		Fix: func(obj *toolchainv1aplha1.SpaceProvisionerConfig) *toolchainv1aplha1.SpaceProvisionerConfig {
			obj.Spec.ToolchainCluster = tc
			return obj
		},
	})
	return a
}

var _ assertions.WithAssertions[*toolchainv1aplha1.SpaceProvisionerConfig] = (*Assertions)(nil)
