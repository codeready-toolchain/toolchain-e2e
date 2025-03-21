package assertions2

import "sigs.k8s.io/controller-runtime/pkg/client"

var _ WithAssertions[int] = (*EmbeddableAssertions[int, int])(nil)

// EmbeddableAssertions is meant to be embedded into other structs as a means for storing the assertions.
// Initialize it using the EmbedInto method.
type EmbeddableAssertions[Self any, T any] struct {
	assertions *[]Assertion[T]
	self       *Self
}

// Self can be used in structs that embed embeddable assertions and are themselves meant to be embedded to return the correct
// type of the struct that embeds them from their fluent methods.
func (a *EmbeddableAssertions[Self, T]) Self() *Self {
	return a.self
}

// WireUp initializes the embeddable assertions struct using a pointer to the assertions array that should be used
// as the storage for the assertions and also a pointer to "self". This is meant to enable returning a correctly typed object
// from the Self method such that all structs that are embedded into some "end user" struct can define fluent methods
// returning the correct type of the "end user".
func (a *EmbeddableAssertions[Self, T]) WireUp(self *Self, assertions *[]Assertion[T]) {
	a.self = self
	a.assertions = assertions
}

// AddAssertion adds the provided assertion to the list of assertions.
func (ea *EmbeddableAssertions[Self, T]) AddAssertion(a Assertion[T]) {
	*ea.assertions = append(*ea.assertions, a)
}

// AddAssertionFunc is a convenience function for the common case of implementing the assertions
// using a simple function.
func (ea *EmbeddableAssertions[Self, T]) AddAssertionFunc(assertion func(AssertT, T)) {
	ea.AddAssertion(AssertionFunc[T](assertion))
}

func (ea *EmbeddableAssertions[Self, T]) Assertions() []Assertion[T] {
	return *ea.assertions
}

func (ea *EmbeddableAssertions[Self, T]) WireableAssertions() *[]Assertion[T] {
	return ea.assertions
}

// Test is a fluent variant to test the assertions on an object.
func (ea *EmbeddableAssertions[Self, T]) Test(t AssertT, obj T) {
	t.Helper()
	Test(t, obj, ea)
}

type EmbeddableObjectAssertions[Self any, T client.Object] struct {
	EmbeddableAssertions[Self, T]
}

func (ea *EmbeddableObjectAssertions[Self, T]) WaitFor(cl client.Client) *Finder[T] {
	return WaitFor[T](cl)
}
