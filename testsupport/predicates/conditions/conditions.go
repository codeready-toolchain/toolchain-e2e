package conditions

import (
	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Predicates are all the predicates that can be defined on any object that has
// conditions.
type Predicates[Self any, T client.Object] interface {
	HasConditionWithType(typ v1alpha1.ConditionType, preds ...predicates.Predicate[v1alpha1.Condition]) Self
}

// WithStatus checks that a condition has the provided status
func WithStatus(status corev1.ConditionStatus) predicates.Predicate[v1alpha1.Condition] {
	return &withStatus{status: status}
}

// ConditionPredicates is a struct implementing the Predicates interface in this package.
// It is meant to be embedded into other types implementing the predicate collectors for
// concrete CRDs.
type ConditionPredicates[Self any, T client.Object] struct {
	predicates.EmbedablePredicates[Self, T]

	// Accessor is a function that translates from the object to its conditions.
	// It returns a pointer to the slice so that the slice can also be initialized
	// when it is nil and append works even if it re-allocates the array.
	accessor func(T) *[]v1alpha1.Condition
}

func (p *ConditionPredicates[Self, T]) HasConditionWithType(typ v1alpha1.ConditionType, predicates ...predicates.Predicate[v1alpha1.Condition]) Self {
	*p.Preds = append(*p.Preds, &testConditionPred[T]{accessor: p.accessor, typ: typ, preds: predicates})
	return p.Self
}

// EmbedInto is a specialized version of the embedding function that sets up the self and predicates but also
// sets the accessor function that translates from an object of type T into its list of conditions.
func (p *ConditionPredicates[Self, T]) EmbedInto(self Self, predicates *[]predicates.Predicate[T], accessor func(T) *[]v1alpha1.Condition) {
	p.EmbedablePredicates.EmbedInto(self, predicates)
	p.accessor = accessor
}

type testConditionPred[T client.Object] struct {
	accessor func(T) *[]v1alpha1.Condition
	typ      v1alpha1.ConditionType
	preds    []predicates.Predicate[v1alpha1.Condition]
}

func (p *testConditionPred[T]) Matches(obj T) (bool, error) {
	conds := p.accessor(obj)
	cond, ok := condition.FindConditionByType(*conds, p.typ)
	if !ok {
		return false, nil
	}

	for _, pred := range p.preds {
		ok, err := pred.Matches(cond)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

func (p *testConditionPred[T]) FixToMatch(obj T) (T, error) {
	copy := obj.DeepCopyObject().(T)
	conds := p.accessor(copy)
	cond, ok := condition.FindConditionByType(*conds, p.typ)
	if !ok {
		return obj, nil
	}

	for _, pred := range p.preds {
		if pred, ok := pred.(predicates.PredicateMatchFixer[v1alpha1.Condition]); ok {
			var err error
			cond, err = pred.FixToMatch(cond)
			if err != nil {
				return obj, err
			}
		}
	}

	*conds, _ = condition.AddOrUpdateStatusConditions(*conds, cond)

	return copy, nil
}

type withStatus struct {
	status corev1.ConditionStatus
}

func (p *withStatus) Matches(cond v1alpha1.Condition) (bool, error) {
	return cond.Status == p.status, nil
}

func (p *withStatus) FixToMatch(cond v1alpha1.Condition) (v1alpha1.Condition, error) {
	// cond is passed by value and is not a pointer so no need to copy
	cond.Status = p.status
	return cond, nil
}
