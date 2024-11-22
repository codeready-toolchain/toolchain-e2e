package predicates

// PredicateCollector is a helper to Waiter struct and its methods, to which it can supply
// a list of predicates to check. An implementation of the PredicateCollector interface
// dictates what kind of predicates can be applied. Therefore, a predicate collector implementation
// is expected to exist for each CRD.
type PredicateCollector[T any] interface {
	Predicates() []Predicate[T]
}

// EmbedablePredicates is meant to be embedded into other structs actually offering some
// predicates for some type of object. It provides the storage for the predicates and also a
// "self-reference" that can be used to return right type of the top level struct when calling
// predicate methods of embedded collectors.
//
// See how this is used in ObjectPredicates and ConditionPredicates which are
// meant to be embedded and SpaceProvisionerConfigPredicates which embeds these two in it.
type EmbedablePredicates[Self any, T any] struct {
	// Self is a typed reference to this instance used to return the correct type from methods
	// of structs embedded in each other.
	//
	// THIS IS ONLY MADE PUBLIC SO THAT IT CAN BE ACCESSED FROM OTHER PACKAGES. DO NOT SET
	// THIS FIELD - USE THE EmbedInto() METHOD.
	Self Self

	// Preds is the list predicates.
	// It returns a pointer to the slice so that the slice can also be initialized
	// when it is nil and append works even if it re-allocates the array.
	//
	// THIS IS ONLY MADE PUBLIC SO THAT IT CAN BE ACCESSED FROM OTHER PACKAGES. DO NOT SET
	// THIS FIELD - USE THE EmbedInto() METHOD.
	Preds *[]Predicate[T]
}

// EmbedInto is a helper function meant to be called by the constructor functions
// to "weave" the pointers to the actual instance that should be used as return type
// of the various predicate functions and the list of predicates that should be common
// to all embedded structs.
func (pc *EmbedablePredicates[Self, T]) EmbedInto(self Self, predicates *[]Predicate[T]) {
	pc.Self = self
	pc.Preds = predicates
}
