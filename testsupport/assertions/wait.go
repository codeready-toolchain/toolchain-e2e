package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kwait "k8s.io/apimachinery/pkg/util/wait"
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

type logger interface {
	Logf(format string, args ...any)
}

type errorCollectingT struct {
	errors []error
	logger
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

func (f *Finder[T]) WithObjectKey(namespace, name string) *FinderByObjectKey[T] {
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

func (t *errorCollectingT) Errorf(format string, args ...interface{}) {
	t.failed = true
	t.errors = append(t.errors, fmt.Errorf(format, args...))
}

func (f *errorCollectingT) Helper() {
	// we cannot call any inner Helper() because that wouldn't work anyway
}

func (f *errorCollectingT) FailNow() {
	panic("assertion failed")
}

func (f *FinderInNamespace[T]) First(ctx context.Context, t RequireT, assertions WithAssertions[T]) T {
	t.Helper()

	t.Logf("waiting for the first object of type %T in namespace '%s' to match criteria", newObject[T](), f.namespace)

	possibleGvks, _, err := f.cl.Scheme().ObjectKinds(newObject[T]())
	require.NoError(t, err)
	require.Len(t, possibleGvks, 1)

	gvk := possibleGvks[0]

	var returnedObject T

	ft := &errorCollectingT{logger: t}

	err = kwait.PollUntilContextTimeout(ctx, f.tick, f.timeout, true, func(ctx context.Context) (done bool, err error) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		ft.errors = nil
		if err := f.cl.List(ctx, list, client.InNamespace(f.namespace)); err != nil {
			return false, err
		}
		for _, uobj := range list.Items {
			uobj := uobj
			obj, err := cast[T](f.cl.Scheme(), &uobj)
			if err != nil {
				return false, fmt.Errorf("failed to cast object with GVK %v to object %T: %w", gvk, newObject[T](), err)
			}

			testInner(ft, obj, assertions, true)

			if !ft.failed {
				returnedObject = obj
			}
		}
		return !ft.failed, nil
	})
	if err != nil {
		sb := strings.Builder{}
		sb.WriteString("failed to find objects (of GVK '%s') in namespace '%s' matching the criteria: %s")
		args := []any{gvk, f.namespace, err.Error()}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		if err := f.cl.List(context.TODO(), list, client.InNamespace(f.namespace)); err != nil {
			sb.WriteString(" and also failed to retrieve the object at all with error: %s")
			args = append(args, err)
		} else {
			sb.WriteString("\nlisting the objects found in cluster with the differences from the expected state for each:")
			for _, o := range list.Items {
				o := o
				obj, _ := cast[T](f.cl.Scheme(), &o)
				key := client.ObjectKeyFromObject(obj)

				sb.WriteRune('\n')
				sb.WriteString("object ")
				sb.WriteString(key.String())
				sb.WriteString(":\n")
				format, oargs := doExplainAfterTestFailure(obj, assertions)
				sb.WriteString(format)
				args = append(args, oargs...)
			}
		}
		t.Logf(sb.String(), args...)
	}

	return returnedObject
}

func (f *FinderByObjectKey[T]) Matching(ctx context.Context, t AssertT, assertions WithAssertions[T]) T {
	t.Helper()

	t.Logf("waiting for %T with name '%s' in namespace '%s' to match additional criteria", newObject[T](), f.key.Name, f.key.Namespace)

	var returnedObject T

	ft := &errorCollectingT{logger: t}

	err := kwait.PollUntilContextTimeout(ctx, f.tick, f.timeout, true, func(ctx context.Context) (done bool, err error) {
		t.Helper()
		ft.errors = nil
		obj := newObject[T]()
		err = f.cl.Get(ctx, f.key, obj)
		if err != nil {
			assert.NoError(ft, err, "failed to find the object by key %s", f.key)
			return false, err
		}

		testInner(ft, obj, assertions, true)

		if !ft.failed {
			returnedObject = obj
		}

		return !ft.failed, nil
	})
	if err != nil {
		if ft.failed {
			for _, e := range ft.errors {
				t.Errorf("%s", e) //nolint: testifylint
			}
			obj := newObject[T]()
			err := f.cl.Get(ctx, f.key, obj)
			if err != nil {
				t.Errorf("failed to find the object while reporting the failure to match by criteria using object key %s", f.key)
				return returnedObject
			}
			format, args := doExplainAfterTestFailure(obj, assertions)
			t.Logf(format, args...)
		}
		t.Logf("couldn't match %T with name '%s' in namespace '%s' with additional criteria because of: %s", newObject[T](), f.key.Name, f.key.Namespace, err)
	}

	return returnedObject
}

func (f *FinderByObjectKey[T]) Deleted(ctx context.Context, t AssertT) {
	t.Helper()

	err := kwait.PollUntilContextTimeout(ctx, f.tick, f.timeout, true, func(ctx context.Context) (done bool, err error) {
		obj := newObject[T]()
		err = f.cl.Get(ctx, f.key, obj)
		if err != nil && apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		assert.Fail(t, "object with key %s still present or other error happened: %s", f.key, err)
	}
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
