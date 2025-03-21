package metadata

import (
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Assertions[Self any, T client.Object] struct {
	assertions.EmbeddableAssertions[Self, T]
}

func (o *Assertions[Self, T]) HasLabel(label string) *Self {
	o.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Contains(t, o.GetLabels(), label, "label '%s' not found", label)
	})
	return o.Self()
}

func (o *Assertions[Self, T]) HasLabelWithValue(label string, value string) *Self {
	o.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Equal(t, value, o.GetLabels()[label])
	})
	return o.Self()
}

func (o *Assertions[Self, T]) HasName(name string) *Self {
	o.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Equal(t, name, o.GetName())
	})
	return o.Self()
}

func (o *Assertions[Self, T]) IsInNamespace(namespace string) *Self {
	o.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Equal(t, namespace, o.GetNamespace())
	})
	return o.Self()
}

func (o *Assertions[Self, T]) WithNameAndNamespace(name, ns string) *Self {
	o.HasName(name)
	return o.IsInNamespace(ns)
}
