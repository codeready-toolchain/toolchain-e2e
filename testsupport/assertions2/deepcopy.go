package assertions2

type deepCopy[T any] interface {
	DeepCopy() T
}

func copyObject[T any](obj any) T {
	if dc, ok := obj.(deepCopy[T]); ok {
		return dc.DeepCopy()
	}
	// TODO: should we go into attempting cloning slices and maps?
	return obj.(T)
}
