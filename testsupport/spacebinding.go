package testsupport

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateSpaceBinding creates SpaceBinding resource for the given MUR & Space with the given space role
func CreateSpaceBinding(t *testing.T, hostAwait *wait.HostAwaitility, mur *v1alpha1.MasterUserRecord, space *v1alpha1.Space, spaceRole string) *v1alpha1.SpaceBinding {
	spaceBinding := NewSpaceBinding(hostAwait.Namespace, mur.Name, space.Name, spaceRole)
	err := hostAwait.CreateWithCleanup(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return spaceBinding
}

// CreateSpaceBindingWithoutCleanup creates SpaceBinding resource for the given MUR & Space with the given space role; and doesn't mark the resource to be ready for cleanup
func CreateSpaceBindingWithoutCleanup(t *testing.T, hostAwait *wait.HostAwaitility, mur *v1alpha1.MasterUserRecord, space *v1alpha1.Space, spaceRole string) *v1alpha1.SpaceBinding {
	spaceBinding := NewSpaceBinding(hostAwait.Namespace, mur.Name, space.Name, spaceRole)
	err := hostAwait.Client.Create(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return spaceBinding
}

// NewSpaceBinding create an object SpaceBinding with the given values
func NewSpaceBinding(namespace, mur, space, spaceRole string) *v1alpha1.SpaceBinding {
	return &v1alpha1.SpaceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", mur, space),
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha1.SpaceBindingMasterUserRecordLabelKey: mur,
				v1alpha1.SpaceBindingSpaceLabelKey:            space,
			},
		},
		Spec: v1alpha1.SpaceBindingSpec{
			MasterUserRecord: mur,
			Space:            space,
			SpaceRole:        spaceRole,
		},
	}
}
