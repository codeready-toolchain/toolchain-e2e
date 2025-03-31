package object

import (
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Assertions[Self any, T client.Object] struct {
	assertions.EmbeddableAssertions[Self, T]
}

func (o *Assertions[Self, T]) HasLabel(label string) *Self {
	o.AddAssertion(&assertions.AssertAndFixFunc[T]{
		Assert: func(t assertions.AssertT, o T) {
			t.Helper()
			t.Logf("ad-hoc log message from within the HasLabel assertion :)")
			assert.Contains(t, o.GetLabels(), label, "label '%s' not found", label)
		},
		Fix: func(o T) T {
			labels := o.GetLabels()
			if labels == nil {
				labels = map[string]string{}
				o.SetLabels(labels)
			}
			labels[label] = ""
			return o
		},
	})
	return o.Self()
}

func (o *Assertions[Self, T]) HasLabelWithValue(label string, value string) *Self {
	o.AddAssertion(&assertions.AssertAndFixFunc[T]{
		Assert: func(t assertions.AssertT, o T) {
			t.Helper()
			assert.Equal(t, value, o.GetLabels()[label])
		},
		Fix: func(o T) T {
			labels := o.GetLabels()
			if labels == nil {
				labels = map[string]string{}
				o.SetLabels(labels)
			}
			labels[label] = value
			return o
		},
	})
	return o.Self()
}

func (o *Assertions[Self, T]) HasName(name string) *Self {
	o.AddAssertion(&assertions.AssertAndFixFunc[T]{
		Assert: func(t assertions.AssertT, o T) {
			t.Helper()
			assert.Equal(t, name, o.GetName())
		},
		Fix: func(o T) T {
			o.SetName(name)
			return o
		},
	})
	return o.Self()
}

func (o *Assertions[Self, T]) IsInNamespace(namespace string) *Self {
	o.AddAssertion(&assertions.AssertAndFixFunc[T]{
		Assert: func(t assertions.AssertT, o T) {
			t.Helper()
			assert.Equal(t, namespace, o.GetNamespace())
		},
		Fix: func(o T) T {
			o.SetNamespace(namespace)
			return o
		},
	})
	return o.Self()
}
