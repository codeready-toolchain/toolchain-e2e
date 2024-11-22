package object

import (
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Predicates defines all the predicates that can be applied to any Object.
type Predicates[Self any, T client.Object] interface {
	HasName(name string) Self
	HasFinalizer(finalizerName string) Self
}

// ObjectPredicates implements the Predicates interface in this package.
// It is not meant to be used directly but rather embedded into other structs that define
// the predicates for individual CRDs.
type ObjectPredicates[Self any, T client.Object] struct {
	predicates.EmbedablePredicates[Self, T]
}

func (p *ObjectPredicates[Self, T]) HasName(name string) Self {
	*p.Preds = append(*p.Preds, &namePredicate[T]{name: name})
	return p.Self
}

func (p *ObjectPredicates[Self, T]) HasFinalizer(finalizerName string) Self {
	*p.Preds = append(*p.Preds, &finalizerPredicate[T]{finalizer: finalizerName})
	return p.Self
}

var (
	_ predicates.Predicate[client.Object]           = (*namePredicate[client.Object])(nil)
	_ predicates.PredicateMatchFixer[client.Object] = (*namePredicate[client.Object])(nil)
	_ predicates.Predicate[client.Object]           = (*finalizerPredicate[client.Object])(nil)
	_ predicates.PredicateMatchFixer[client.Object] = (*finalizerPredicate[client.Object])(nil)
)

type namePredicate[T client.Object] struct {
	name string
}

func (p *namePredicate[T]) Matches(obj T) (bool, error) {
	return p.name == obj.GetName(), nil
}

func (p *namePredicate[T]) FixToMatch(obj T) (T, error) {
	obj = obj.DeepCopyObject().(T)
	obj.SetName(p.name)
	return obj, nil
}

type finalizerPredicate[T client.Object] struct {
	finalizer string
}

func (p *finalizerPredicate[T]) Matches(obj T) (bool, error) {
	return slices.Contains(obj.GetFinalizers(), p.finalizer), nil
}

func (p *finalizerPredicate[T]) FixToMatch(obj T) (T, error) {
	obj = obj.DeepCopyObject().(T)
	fs := obj.GetFinalizers()
	if !slices.Contains(fs, p.finalizer) {
		fs = append(fs, p.finalizer)
	}
	obj.SetFinalizers(fs)
	return obj, nil
}
