package metadata

import (
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MetadataAssertions is a set of assertions on the metadata of any client.Object.
type MetadataAssertions struct {
	assertions.Assertions[client.Object]
}

// With is a "readable" constructor of MetadataAssertions. It is meant to be used
// to construct the MetadataAssertions instance so that the call reads like an English
// sentence: "metadata.With().Name().Namespace()..."
func With() *MetadataAssertions {
	return &MetadataAssertions{}
}

// objectName is a special impl of an assertion on object name that also implements
// the assertions.ObjectNameAssertion so that it can be used in await methods to
// identify the object.
type objectName struct {
	name string
}

// objectName is a special impl of an assertion on object name that also implements
// the assertions.ObjectNamespaceAssertion so that it can be used in await methods to
// identify the object.
type objectNamespace struct {
	namespace string
}

// Name adds an assertion on the objects name being equal to the provided value.
// The assertion also implements the assertions.ObjectNameAssertion so that it can be
// transparently used to identify the object during the assertions.Await calls.
func (ma *MetadataAssertions) Name(name string) *MetadataAssertions {
	ma.Assertions = assertions.Append(ma.Assertions, &objectName{name: name})
	return ma
}

// Name adds an assertion on the objects namespace being equal to the provided value.
// The assertion also implements the assertions.ObjectNamespaceAssertion so that it can be
// transparently used to identify the object during the assertions.Await calls.
func (ma *MetadataAssertions) Namespace(ns string) *MetadataAssertions {
	ma.Assertions = assertions.Append(ma.Assertions, &objectNamespace{namespace: ns})
	return ma
}

// Label adds an assertion for the presence of the label on the object.
func (ma *MetadataAssertions) Label(name string) *MetadataAssertions {
	ma.Assertions = assertions.AppendFunc(ma.Assertions, func(t assertions.AssertT, obj client.Object) {
		t.Helper()
		assert.Contains(t, obj.GetLabels(), name, "no label called '%s' found on the object", name)
	})
	return ma
}

func (ma *MetadataAssertions) NoLabel(name string) *MetadataAssertions {
	ma.Assertions = assertions.AppendFunc(ma.Assertions, func(t assertions.AssertT, obj client.Object) {
		t.Helper()
		assert.NotContains(t, obj.GetLabels(), name, "a label called '%s' found on the object but none expected", name)
	})
	return ma
}

func (a *objectName) Test(t assertions.AssertT, obj client.Object) {
	t.Helper()
	assert.Equal(t, a.name, obj.GetName(), "object name doesn't match")
}

func (a *objectName) Name() string {
	return a.name
}

func (a *objectNamespace) Test(t assertions.AssertT, obj client.Object) {
	t.Helper()
	assert.Equal(t, a.namespace, obj.GetNamespace(), "object namespace doesn't match")
}

func (a *objectNamespace) Namespace() string {
	return a.namespace
}

var (
	_ assertions.Assertion[client.Object] = (*objectName)(nil)
	_ assertions.Assertion[client.Object] = (*objectNamespace)(nil)
	_ assertions.ObjectNameAssertion      = (*objectName)(nil)
	_ assertions.ObjectNamespaceAssertion = (*objectNamespace)(nil)
)
