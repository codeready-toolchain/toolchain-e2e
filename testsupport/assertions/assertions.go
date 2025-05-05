package assertions

// Assertion is a test function that is meant to test some object.
type Assertion[T any] interface {
	Test(t AssertT, obj T)
}

// Assertions is just a list of assertions provided for convenience. It is meant to be embedded into structs.
type Assertions[T any] []Assertion[T]

// AssertionFunc converts a function into an assertion.
type AssertionFunc[T any] func(t AssertT, obj T)

// Append is just an alias for Go's built-in append.
func Append[Type any](assertionList Assertions[Type], assertions ...Assertion[Type]) Assertions[Type] {
	assertionList = append(assertionList, assertions...)
	return assertionList
}

// AppendGeneric is a variant of append that can also cast assertions on some super-type into assertions
// on some type. This can be useful when one has some assertions that work on an super-type of some type and
// you want to append it to a list of assertions on the type itself.
func AppendGeneric[SuperType any, Type any](assertionList Assertions[Type], assertions ...Assertion[SuperType]) Assertions[Type] {
	for _, a := range assertions {
		assertionList = append(assertionList, CastAssertion[SuperType, Type](a))
	}
	return assertionList
}

// AppendLifted is a convenience function to first lift all the assertions to the "To" type and then append them to the provided list.
func AppendLifted[From any, To any](conversion func(To) (From, bool), assertionList Assertions[To], assertions ...Assertion[From]) Assertions[To] {
	return Append(assertionList, LiftAll(conversion, assertions...)...)
}

// AppendFunc is a convenience function that is able to take in the assertions as simple functions.
func AppendFunc[T any](assertionList Assertions[T], fn ...AssertionFunc[T]) Assertions[T] {
	for _, f := range fn {
		assertionList = append(assertionList, f)
	}
	return assertionList
}

// Test runs the test by all assertions in the list.
func (as Assertions[T]) Test(t AssertT, obj T) {
	t.Helper()
	for _, a := range as {
		a.Test(t, obj)
	}
}

func (f AssertionFunc[T]) Test(t AssertT, obj T) {
	t.Helper()
	f(t, obj)
}
