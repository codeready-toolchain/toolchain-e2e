package assertions

// CastAssertion can be used to convert a generic assertion on, say, client.Object, into
// an assertion on a concrete subtype. Note that the conversion is not guaranteed to
// pass by the type system and can fail at runtime.
func CastAssertion[SuperType any, Type any](a Assertion[SuperType]) Assertion[Type] {
	// we cannot pass "cast[SuperType]" as a function pointer, so we need this aid
	conversion := func(o Type) (SuperType, bool) {
		return cast[SuperType](o)
	}

	return Convert(conversion, a)
}

// Convert converts from one assertion type to another by converting the tested value.
// It respectes the ObjectNameAssertion and ObjectNamespaceAssertion so that assertions
// can still be used to identify the object after conversion.
// The provided accessor can be fallible, returning false on the failure to convert the object.
func Convert[From any, To any](accessor func(To) (From, bool), assertion Assertion[From]) Assertion[To] {
	if _, ok := assertion.(ObjectNameAssertion); ok {
		return &convertedObjectName[From, To]{convertedAssertion: convertedAssertion[From, To]{accessor: accessor, assertion: assertion}}
	} else if _, ok := assertion.(ObjectNamespaceAssertion); ok {
		return &convertedObjectNamespace[From, To]{convertedAssertion: convertedAssertion[From, To]{accessor: accessor, assertion: assertion}}
	} else {
		return &convertedAssertion[From, To]{accessor: accessor, assertion: assertion}
	}
}

// ConvertAll performs Convert on all the provided assertions.
func ConvertAll[From any, To any](accessor func(To) (From, bool), assertions ...Assertion[From]) Assertions[To] {
	tos := make(Assertions[To], len(assertions))
	for i, a := range assertions {
		tos[i] = Convert(accessor, a)
	}
	return tos
}

// cast casts the obj into T. This is strangely required in cases where you want to cast
// object that is typed using a type parameter into a type specified by another type parameter.
// The compiler rejects such casts but doesn't complain if the cast is done using
// an indirection using this function.
func cast[T any](obj any) (T, bool) {
	ret, ok := obj.(T)
	return ret, ok
}

type convertedAssertion[From any, To any] struct {
	assertion Assertion[From]
	accessor  func(To) (From, bool)
}

func (ca *convertedAssertion[From, To]) Test(t AssertT, obj To) {
	t.Helper()
	o, ok := ca.accessor(obj)
	if !ok {
		t.Errorf("invalid conversion")
		return
	}
	ca.assertion.Test(t, o)
}

type convertedObjectName[From any, To any] struct {
	convertedAssertion[From, To]
}

func (con *convertedObjectName[From, To]) Name() string {
	return con.assertion.(ObjectNameAssertion).Name()
}

type convertedObjectNamespace[From any, To any] struct {
	convertedAssertion[From, To]
}

func (con *convertedObjectNamespace[From, To]) Namespace() string {
	return con.assertion.(ObjectNamespaceAssertion).Namespace()
}
