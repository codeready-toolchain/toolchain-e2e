package assertions

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Assertion[T any] func(t AssertT, obj T)

type EmbeddableAssertions[Self any, T any] struct {
	assertions *[]Assertion[T]
	self       *Self
}

type WithAssertions[T any] interface {
	Assertions() []Assertion[T]
}

type AssertT interface {
	assert.TestingT
	Helper()
}

type RequireT interface {
	require.TestingT
	Helper()
}

func Test[T any, A WithAssertions[T]](t AssertT, obj T, assertions A) {
	t.Helper()
	for _, a := range assertions.Assertions() {
		a(t, obj)
	}
}

func (a *EmbeddableAssertions[Self, T]) Self() *Self {
	return a.self
}

func (a *EmbeddableAssertions[Self, T]) EmbedInto(self *Self, assertions *[]Assertion[T]) {
	a.self = self
	a.assertions = assertions
}

func (ea *EmbeddableAssertions[Self, T]) AddAssertion(a Assertion[T]) {
	*ea.assertions = append(*ea.assertions, a)
}
