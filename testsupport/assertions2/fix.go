package assertions2

import (
	"strings"

	"github.com/google/go-cmp/cmp"
)

var (
	_ Assertion[bool]      = (*AssertAndFixFunc[bool])(nil)
	_ AssertionFixer[bool] = (*AssertAndFixFunc[bool])(nil)
)

func Explain[T any, A WithAssertions[T]](obj T, assertions A) string {
	cpy := copyObject[T](obj)

	modified := false
	for _, a := range assertions.Assertions() {
		if f, ok := a.(AssertionFixer[T]); ok {
			f.AdaptToMatch(cpy)
			modified = true
		}
	}

	if modified {
		sb := strings.Builder{}
		sb.WriteString(cmp.Diff(obj, cpy))

		return sb.String()
	}

	return ""
}

type AssertionFixer[T any] interface {
	AdaptToMatch(object T) T
}

type AssertAndFixFunc[T any] struct {
	Assert func(t AssertT, obj T)
	Fix    func(obj T) T
}

func (a *AssertAndFixFunc[T]) Test(t AssertT, obj T) {
	t.Helper()
	if a.Assert != nil {
		a.Assert(t, obj)
	}
}

func (a *AssertAndFixFunc[T]) AdaptToMatch(object T) T {
	if a.Fix != nil {
		return a.Fix(object)
	}
	return object
}
