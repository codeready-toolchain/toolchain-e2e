package conditions

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/stretchr/testify/assert"
)

type ConditionAssertions struct {
	assertions.Assertions[[]toolchainv1alpha1.Condition]
}

func With() *ConditionAssertions {
	return &ConditionAssertions{}
}

func (cas *ConditionAssertions) Type(typ toolchainv1alpha1.ConditionType) *ConditionAssertions {
	cas.Assertions = assertions.AppendFunc(cas.Assertions, func(t assertions.AssertT, conds []toolchainv1alpha1.Condition) {
		t.Helper()
		_, found := condition.FindConditionByType(conds, typ)
		assert.True(t, found, "didn't find a condition with the type '%v'", typ)
	})
	return cas
}
