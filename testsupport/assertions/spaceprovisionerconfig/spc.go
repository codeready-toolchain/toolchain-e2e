package spaceprovisionerconfig

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/conditions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/object"
	"github.com/stretchr/testify/assert"
)

type SpaceProvisionerConfigAssertions struct {
	object.ObjectAssertions[SpaceProvisionerConfigAssertions, *toolchainv1alpha1.SpaceProvisionerConfig]
}

func Asserting() *SpaceProvisionerConfigAssertions {
	spc := &SpaceProvisionerConfigAssertions{}
	spc.SetFluentSelf(spc)
	return spc
}

func (spc *SpaceProvisionerConfigAssertions) Conditions(cas *conditions.ConditionAssertions) *SpaceProvisionerConfigAssertions {
	spc.Assertions = assertions.AppendConverted(getConditions, spc.Assertions, cas.Assertions...)
	return spc
}

func (spc *SpaceProvisionerConfigAssertions) ToolchainClusterName(name string) *SpaceProvisionerConfigAssertions {
	spc.Assertions = assertions.AppendFunc(spc.Assertions, func(t assertions.AssertT, obj *toolchainv1alpha1.SpaceProvisionerConfig) {
		t.Helper()
		assert.Equal(t, name, obj.Spec.ToolchainCluster, "unexpected toolchainCluster")
	})
	return spc
}

func getConditions(spc *toolchainv1alpha1.SpaceProvisionerConfig) ([]toolchainv1alpha1.Condition, bool) {
	return spc.Status.Conditions, true
}
