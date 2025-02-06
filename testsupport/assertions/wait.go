package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitFor[T client.Object](cl client.Client) *Finder[T] {
	return &Finder[T]{
		cl:      cl,
		timeout: wait.DefaultTimeout,
		tick:    wait.DefaultRetryInterval,
	}
}

type Finder[T client.Object] struct {
	cl      client.Client
	timeout time.Duration
	tick    time.Duration
}

type FinderInNamespace[T client.Object] struct {
	Finder[T]
	namespace string
}

type FinderByObjectKey[T client.Object] struct {
	Finder[T]
	key client.ObjectKey
}

type failureTrackingT struct {
	*assert.CollectT
	failed bool
}

func (f *Finder[T]) WithTimeout(timeout time.Duration) *Finder[T] {
	f.timeout = timeout
	return f
}

func (f *Finder[T]) WithRetryInterval(interval time.Duration) *Finder[T] {
	f.tick = interval
	return f
}

func (f *Finder[T]) ByObjectKey(namespace, name string) *FinderByObjectKey[T] {
	return &FinderByObjectKey[T]{
		Finder: *f,
		key:    client.ObjectKey{Name: name, Namespace: namespace},
	}
}

func (f *Finder[T]) InNamespace(ns string) *FinderInNamespace[T] {
	return &FinderInNamespace[T]{
		Finder:    *f,
		namespace: ns,
	}
}

func (f *FinderInNamespace[T]) WithName(name string) *FinderByObjectKey[T] {
	return &FinderByObjectKey[T]{
		Finder: f.Finder,
		key:    client.ObjectKey{Name: name, Namespace: f.namespace},
	}
}

func (t *failureTrackingT) Errorf(format string, args ...interface{}) {
	t.failed = true
	t.CollectT.Errorf(format, args...)
}

func (f *failureTrackingT) Helper() {
	// this is a wrapper of CollectT so helper should do nothing
}

func (f *FinderInNamespace[T]) First(ctx context.Context, t RequireT, assertions WithAssertions[T]) T {
	t.Helper()

	possibleGvks, _, err := f.cl.Scheme().ObjectKinds(newObject[T]())
	require.NoError(t, err)
	require.Len(t, possibleGvks, 1)

	gvk := possibleGvks[0]

	var returnedObject T

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		require.NoError(c, f.cl.List(ctx, list, client.InNamespace(f.namespace)))
		for _, uobj := range list.Items {
			uobj := uobj
			obj, err := cast[T](f.cl.Scheme(), &uobj)
			require.NoError(c, err)

			f := &failureTrackingT{CollectT: c}

			Test(f, obj, assertions)

			if !f.failed {
				returnedObject = obj
			}
		}
	}, f.timeout, f.tick) // some more thorough message should be added here as a param to Eventually

	return returnedObject
}

func (f *FinderByObjectKey[T]) Matching(ctx context.Context, t assert.TestingT, assertions WithAssertions[T]) T {
	if t, ok := t.(interface{ Helper() }); ok {
		t.Helper()
	}

	var returnedObject T

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		obj := newObject[T]()
		err := f.cl.Get(ctx, f.key, obj)
		if err != nil {
			assert.NoError(c, err, "failed to find the object by key %s", f.key)
			return
		}

		f := &failureTrackingT{CollectT: c}

		Test(f, obj, assertions)

		if !f.failed {
			returnedObject = obj
		}
	}, f.timeout, f.tick)

	return returnedObject
}

func (f *FinderByObjectKey[T]) Deleted(ctx context.Context, t assert.TestingT) {
	if t, ok := t.(interface{ Helper() }); ok {
		t.Helper()
	}

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		obj := newObject[T]()
		err := f.cl.Get(ctx, f.key, obj)
		if err != nil && apierrors.IsNotFound(err) {
			return
		}
		assert.Fail(c, "object with key %s still present or other error happened: %s", f.key, err)
	}, f.timeout, f.tick)
}

func cast[T client.Object](scheme *runtime.Scheme, obj *unstructured.Unstructured) (T, error) {
	var empty T
	raw, err := obj.MarshalJSON()
	if err != nil {
		return empty, fmt.Errorf("failed to obtain the raw JSON of the object: %w", err)
	}

	typed, err := scheme.New(obj.GroupVersionKind())
	if err != nil {
		return empty, fmt.Errorf("failed to create a new empty object from the scheme: %w", err)
	}

	err = json.Unmarshal(raw, typed)
	if err != nil {
		return empty, fmt.Errorf("failed to unmarshal the raw JSON to the go structure: %w", err)
	}

	return typed.(T), nil
}

func newObject[T client.Object]() T {
	// all client.Object implementations are pointers, so this declaration gives us just a nil pointer
	var v T

	ptrT := reflect.TypeOf(v)
	valT := ptrT.Elem()
	ptrToZeroV := reflect.New(valT)

	zero := ptrToZeroV.Interface()

	return zero.(T)
}
