package wait_test

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStringifyObject(t *testing.T) {
	// given
	now, err := time.Parse("2006-01-02T03:04:05Z", "2021-12-16T10:45:30Z")
	require.NoError(t, err)
	obj := &toolchainv1alpha1.Space{
		TypeMeta: metav1.TypeMeta{
			Kind: "Space",
		},
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(now),
			Name:              "oddity-1",
			ManagedFields: []metav1.ManagedFieldsEntry{
				// whatever
				{
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{},
				},
			},
		},
		Spec: toolchainv1alpha1.SpaceSpec{
			TierName: "base",
		},
	}
	// whenever
	result, err := wait.StringifyObject(obj)
	// then
	require.NoError(t, err)
	assert.Equal(t, `kind: Space
metadata:
  creationTimestamp: "2021-12-16T10:45:30Z"
  name: oddity-1
spec:
  tierName: base
status: {}
`, string(result))
}

func TestStringifyObjects(t *testing.T) {
	// given
	now, err := time.Parse("2006-01-02T03:04:05Z", "2021-12-16T10:45:30Z")
	require.NoError(t, err)
	list := &toolchainv1alpha1.SpaceList{
		Items: []toolchainv1alpha1.Space{
			{
				TypeMeta: metav1.TypeMeta{
					Kind: "Space",
				},
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(now),
					Name:              "oddity-1",
					ManagedFields: []metav1.ManagedFieldsEntry{
						// whatever
						{
							FieldsType: "FieldsV1",
							FieldsV1:   &metav1.FieldsV1{},
						},
					},
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					TierName: "base",
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind: "Space",
				},
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(now),
					Name:              "oddity-2",
					ManagedFields: []metav1.ManagedFieldsEntry{
						// whatever
						{
							FieldsType: "FieldsV1",
							FieldsV1:   &metav1.FieldsV1{},
						},
					},
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					TierName: "advanced",
				},
			},
		},
	}
	// whenever
	result, err := wait.StringifyObjects(list)
	// then
	require.NoError(t, err)
	assert.Equal(t, `- kind: Space
  metadata:
    creationTimestamp: "2021-12-16T10:45:30Z"
    name: oddity-1
  spec:
    tierName: base
  status: {}
- kind: Space
  metadata:
    creationTimestamp: "2021-12-16T10:45:30Z"
    name: oddity-2
  spec:
    tierName: advanced
  status: {}
`, string(result))
}
