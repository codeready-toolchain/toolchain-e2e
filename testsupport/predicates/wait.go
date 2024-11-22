package predicates

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitFor[T client.Object](t *testing.T, cl client.Client) *Waiter[T] {
	return &Waiter[T]{cl: cl}
}

type Waiter[T client.Object] struct {
	cl client.Client
}

func (w *Waiter[T]) First(preds PredicateCollector[T]) (T, error) {
	_ = preds.Predicates()

	// once we have the list of predicates, the implemetation of this method will be very
	// similar to what is already present in the wait package.
	// Or rather, we will change the impl in the wait package to accept the PredicateCollector[T].

	// using this will read as:
	//
	// wait.WaitFor[*toolchainv1alpha1.SpaceProvisonerConfig](t, cl).First(spaceprovisionerconfig.That().HasName(...)...)
	panic("not implemented")
}
