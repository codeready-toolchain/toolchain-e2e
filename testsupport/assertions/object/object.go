package object

import (
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectAssertions is a common base for assertions on client.Object subtypes.
// It provides assertions on the object metadata.
type ObjectAssertions[Self any, T client.Object] struct {
	assertions.AddressableObjectAssertions[T]
	self *Self
}

// SetFluentSelf sets the "Self" that should be returned from the fluent methods like
// ObjectMeta().
func (oa *ObjectAssertions[Self, T]) SetFluentSelf(self *Self) {
	oa.self = self
}

// ObjectMeta sets the assertions on the metadata of the object.
func (oa *ObjectAssertions[Self, T]) ObjectMeta(mas *metadata.MetadataAssertions) *Self {
	oa.Assertions = assertions.AppendGeneric(oa.Assertions, mas.Assertions...)
	return oa.self
}
