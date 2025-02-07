package assertions

var _ Assertion[bool] = (AssertionFunc[bool])(nil)

type Assertion[T any] interface {
	Test(t AssertT, obj T)
}

type AssertionFunc[T any] func(t AssertT, obj T)

type EmbeddableAssertions[Self any, T any] struct {
	assertions *[]Assertion[T]
	self       *Self
}

type WithAssertions[T any] interface {
	Assertions() []Assertion[T]
}

func Test[T any, A WithAssertions[T]](t AssertT, obj T, assertions A) {
	t.Helper()
	testInner(t, obj, assertions, false)
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
	format = "Some of the assertions failed to match the object (see output above). The following diff shows what the object should have looked like:\n%s"
	args = []any{diff}
	return
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

func (f AssertionFunc[T]) Test(t AssertT, obj T) {
	t.Helper()
	f(t, obj)
}
