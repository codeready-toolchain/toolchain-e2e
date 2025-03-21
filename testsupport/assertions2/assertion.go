package assertions2

var _ Assertion[bool] = (AssertionFunc[bool])(nil)

// Assertion is a functional interface that is used to test whether an object satisfies some condition.
type Assertion[T any] interface {
	Test(t AssertT, obj T)
}

// AssertionFunc converts a function into an assertion.
type AssertionFunc[T any] func(t AssertT, obj T)

// WithAssertions is an interface for "things" that make available a list of assertions to use in the Test function.
type WithAssertions[T any] interface {
	Assertions() []Assertion[T]
}

func Test[T any, A WithAssertions[T]](t AssertT, obj T, assertions A) {
	t.Helper()
	testInner(t, obj, assertions, false)
}

func CastAssertion[Type any, SubType any](a Assertion[Type]) Assertion[SubType] {
	if af, ok := a.(AssertionFixer[Type]); ok {
		// convert the assertion fixer, too
		return &AssertAndFixFunc[SubType]{
			Assert: func(t AssertT, obj SubType) {
				t.Helper()
				tobj, ok := cast[Type](obj)
				if !ok {
					t.Errorf("invalid cast")
				}

				a.Test(t, tobj)
			},
			Fix: func(obj SubType) SubType {
				tobj, ok := cast[Type](obj)
				if ok {
					tobj = af.AdaptToMatch(tobj)
					obj, _ = cast[SubType](tobj)
				}
				return obj
			},
		}
	} else {
		// simple
		return AssertionFunc[SubType](func(t AssertT, obj SubType) {
			t.Helper()
			sobj, ok := cast[Type](obj)
			if !ok {
				t.Errorf("invalid cast")
			}

			a.Test(t, sobj)
		})
	}
}

// cast casts the obj into T. This is strangely required in cases where you want to cast
// object that is typed using a type parameter into a type specified by another type parameter.
// The compiler rejects such casts but doesn't complain if the cast is done using
// an indirection using this function.
func cast[T any](obj any) (T, bool) {
	ret, ok := obj.(T)
	return ret, ok
}

func testInner[T any, A WithAssertions[T]](t AssertT, obj T, assertions A, suppressLogAround bool) {
	t.Helper()
	ft := &failureTrackingT{AssertT: t}

	if !suppressLogAround {
		t.Logf("About to test object %T with assertions", obj)
	}

	for _, a := range assertions.Assertions() {
		a.Test(ft, obj)
	}

	if !suppressLogAround && ft.failed {
		format, args := doExplainAfterTestFailure(obj, assertions)
		t.Logf(format, args...)
	}
}

func doExplainAfterTestFailure[T any, A WithAssertions[T]](obj T, assertions A) (format string, args []any) {
	diff := Explain(obj, assertions)
	if diff != "" {
		format = "Some of the assertions failed to match the object (see output above). The following diff shows what the object should have looked like:\n%s"
		args = []any{diff}
	} else {
		format = "Some of the assertions failed to match the object (see output above)."
		args = []any{}
	}

	return
}

func (f AssertionFunc[T]) Test(t AssertT, obj T) {
	t.Helper()
	f(t, obj)
}
