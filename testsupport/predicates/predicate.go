package predicates

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Predicate is a generic predicate for testing whether some object of type T has some quality.
// It is best used with the `wait.For(...).FirstThat(...)` function using the Is function as
// a helper to satisfy the method signatures and help Go's type inference.
type Predicate[T client.Object] interface {
	Matches(obj T) bool
}

// Is merely casts the generic predicate on type T to a predicate on client.Object. This is
// always valid because T is required to implement client.Object. Using this function helps
// readability of the code by being able to construct expressions like:
//
//	predicates.Is(predicates.Named("whatevs"))
//
// Note that if you're implementing your own predicates, it is helpful for the constructor
// function to not return a concrete type but the generic Predicate[YourObjectType]. This
// helps the Go compiler to be able to infer and match up the types correctly.
//
// E.g. if one would want to implement a predicate checking that a ToolchainCluster CR has
// the ready condition checked, one might implement a constructor function for that predicate
// like this:
//
//	type toochainClusterReady struct {}
//
//	func (t *toolchainClusterReady) Matches(c *toolchainv1alpha1.ToolchainCluster) bool {
//	  return condition.IsTrue(c.Status.Conditions, toolchainv1alpha1.ConditionReady)
//	}
//
//	func Ready() predicates.Predicate[*toolchainv1alpha1.ToolchainCluster] {
//	  return &toolchainClusterReady{}
//	}
//
// The Ready() constructor function could then be used as:
//
//	wait.For(t, host.Awaitility, &toolchainv1alpha1.ToolchainCluster{}).FirstThat(predicates.Is(Ready()))
func Is[T client.Object](p Predicate[T]) Predicate[client.Object] {
	return isCast[T]{inner: p}
}

type isCast[T client.Object] struct {
	inner Predicate[T]
}

func (i isCast[T]) Matches(obj client.Object) bool {
	return i.inner.Matches(obj.(T))
}

type named struct {
	name string
}

func (n *named) Matches(obj client.Object) bool {
	return obj.GetName() == n.name
}

// Named returns a predicate checking that an Object has given name.
func Named(name string) Predicate[client.Object] {
	return &named{name: name}
}

type inNamespace struct {
	namespace string
}

func (i *inNamespace) Matches(obj client.Object) bool {
	return obj.GetNamespace() == i.namespace
}

// InNamespace returns a predicate checking that an Object is in the given namespace.
func InNamespace(name string) Predicate[client.Object] {
	return &inNamespace{namespace: name}
}

type withKey struct {
	types.NamespacedName
}

func (w *withKey) Matches(obj client.Object) bool {
	return obj.GetName() == w.Name && obj.GetNamespace() == w.Namespace
}

// WithObjectKey returns a predicate checking that an Object has given NamespacedName (aka client.ObjectKey).
func WithObjectKey(key types.NamespacedName) Predicate[client.Object] {
	return &withKey{NamespacedName: key}
}

type hasLabels struct {
	requiredLabels map[string]string
}

func (h *hasLabels) Matches(obj client.Object) bool {
	objLabels := obj.GetLabels()
	for k, v := range h.requiredLabels {
		value, present := objLabels[k]
		if !present || value != v {
			return false
		}
	}
	return true
}

// WithLabels returns a predicate checking that an Object has provided labels and their values.
func WithLabels(requiredLabels map[string]string) Predicate[client.Object] {
	return &hasLabels{requiredLabels: requiredLabels}
}

type hasAnnotations struct {
	requiredAnnotations map[string]string
}

func (h *hasAnnotations) Matches(obj client.Object) bool {
	objAnnos := obj.GetAnnotations()
	for k, v := range h.requiredAnnotations {
		value, present := objAnnos[k]
		if !present || value != v {
			return false
		}
	}
	return true
}

// WithAnnotations returns a predicate checking that an Object has provided annotations and their values.
func WithAnnotations(requiredAnnotations map[string]string) Predicate[client.Object] {
	return &hasAnnotations{requiredAnnotations: requiredAnnotations}
}
