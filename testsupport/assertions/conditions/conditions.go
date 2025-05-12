package conditions

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
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

func (cas *ConditionAssertions) Status(typ toolchainv1alpha1.ConditionType, status corev1.ConditionStatus) *ConditionAssertions {
	cas.Assertions = assertions.AppendFunc(cas.Assertions, func(t assertions.AssertT, conds []toolchainv1alpha1.Condition) {
		t.Helper()
		cond, found := condition.FindConditionByType(conds, typ)
		assert.True(t, found, "didn't find a condition with the type '%v'", typ)
		assert.Equal(t, status, cond.Status, "condition of type '%v' doesn't have the expected status", typ)
	})
	return cas
}

func (cas *ConditionAssertions) StatusAndReason(typ toolchainv1alpha1.ConditionType, status corev1.ConditionStatus, reason string) *ConditionAssertions {
	cas.Assertions = assertions.AppendFunc(cas.Assertions, func(t assertions.AssertT, conds []toolchainv1alpha1.Condition) {
		t.Helper()
		cond, found := condition.FindConditionByType(conds, typ)
		assert.True(t, found, "didn't find a condition with the type '%v'", typ)
		assert.Equal(t, status, cond.Status, "condition of type '%v' doesn't have the expected status", typ)
		assert.Equal(t, reason, cond.Reason, "condition of type '%v' doesn't have the expected reason", typ)
	})
	return cas
}
