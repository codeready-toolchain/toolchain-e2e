package metadata

import (
	assertions "github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions2"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Assertions[Self any, T client.Object] struct {
	assertions.EmbeddableAssertions[Self, T]
}

func (o *Assertions[Self, T]) Like(ma *ChainingMetadataAssert) *Self {
	for _, a := range ma.Assertions() {
		o.AddAssertion(assertions.CastAssertion[client.Object, T](a))
	}
	return o.Self()
}

func (o *Assertions[Self, T]) With() *NestedMetadataAssert[Self, T] {
	nested := &NestedMetadataAssert[Self, T]{parentSelf: o.Self()}
	assertions := o.WireableAssertions()
	nested.WireUp(nested, assertions)
	return nested
}

type NestedMetadataAssert[P any, T client.Object] struct {
	parentSelf *P
	assertions.EmbeddableAssertions[NestedMetadataAssert[P, T], T]
}

func (nma *NestedMetadataAssert[P, T]) Done() *P {
	return nma.parentSelf
}

func (nma *NestedMetadataAssert[P, T]) Label(name string) *NestedMetadataAssert[P, T] {
	nma.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Contains(t, o.GetLabels(), name, "label '%s' not found", name)
	})
	return nma
}

func (nma *NestedMetadataAssert[P, T]) Name(name string) *NestedMetadataAssert[P, T] {
	nma.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Equal(t, o.GetName(), name)
	})
	return nma
}

func (nma *NestedMetadataAssert[P, T]) Namespace(ns string) *NestedMetadataAssert[P, T] {
	nma.AddAssertionFunc(func(t assertions.AssertT, o T) {
		t.Helper()
		assert.Equal(t, o.GetNamespace(), ns)
	})
	return nma
}

func (nma *NestedMetadataAssert[P, T]) NameAndNamespace(name, ns string) *NestedMetadataAssert[P, T] {
	nma.Name(name)
	return nma.Namespace(ns)
}

type ChainingMetadataAssert struct {
	assertions.EmbeddableAssertions[ChainingMetadataAssert, client.Object]
}

func With() *ChainingMetadataAssert {
	ma := &ChainingMetadataAssert{}
	ma.WireUp(ma, &[]assertions.Assertion[client.Object]{})
	return ma
}

func (ma *ChainingMetadataAssert) Label(name string) *ChainingMetadataAssert {
	ma.AddAssertionFunc(func(t assertions.AssertT, o client.Object) {
		t.Helper()
		assert.Contains(t, o.GetLabels(), name, "label '%s' not found", name)
	})
	return ma
}

func (ma *ChainingMetadataAssert) Name(name string) *ChainingMetadataAssert {
	ma.AddAssertionFunc(func(t assertions.AssertT, o client.Object) {
		t.Helper()
		assert.Equal(t, o.GetName(), name)
	})
	return ma
}

func (ma *ChainingMetadataAssert) Namespace(ns string) *ChainingMetadataAssert {
	ma.AddAssertionFunc(func(t assertions.AssertT, o client.Object) {
		t.Helper()
		assert.Equal(t, o.GetNamespace(), ns)
	})
	return ma
}

func (ma *ChainingMetadataAssert) NameAndNamespace(name, ns string) *ChainingMetadataAssert {
	ma.Name(name)
	return ma.Namespace(ns)
}
