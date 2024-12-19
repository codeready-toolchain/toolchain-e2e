package predicates

import (
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
)

// Predicate tests if an instance of some type matches it. It is fallible so that
// we can do weird stuff like pinging the route endpoints and not have to be weird
// about error handling in such predicates.
type Predicate[T any] interface {
	Matches(T) (bool, error)
}

type PredicateMatchFixer[T any] interface {
	// FixToMatch repares the provided object to match the predicate. It needs to return
	// a copy of the provided object so care needs to be taken if working with slices or
	// pointers.
	FixToMatch(T) (T, error)
}

func Explain[T any](obj T, predicate Predicate[T]) (string, error) {
	predicateType := reflect.TypeOf(predicate)
	if predicateType.Kind() == reflect.Pointer {
		predicateType = predicateType.Elem()
	}

	prefix := fmt.Sprintf("predicate '%s' didn't match the object", predicateType.String())
	fix, ok := predicate.(PredicateMatchFixer[T])
	if !ok {
		return prefix, nil
	}

	expected, err := fix.FixToMatch(obj)
	if err != nil {
		return prefix, err
	}
	diff := cmp.Diff(expected, obj)

	return fmt.Sprintf("%s because of the following differences (- indicates the expected values, + the actual values):\n%s", prefix, diff), nil
}
