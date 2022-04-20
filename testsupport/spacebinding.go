package testsupport

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerifySpaceBinding waits until a spacebinding with the given mur and space name exists and then verifies the contents are correct
func VerifySpaceBinding(t *testing.T, hostAwait *wait.HostAwaitility, murName, spaceName, spaceRole string) *toolchainv1alpha1.SpaceBinding {

	spaceBinding, err := hostAwait.WaitForSpaceBinding(murName, spaceName,
		wait.UntilSpaceBindingHasMurName(murName),
		wait.UntilSpaceBindingHasSpaceName(spaceName),
		wait.UntilSpaceBindingHasSpaceRole(spaceRole),
	)

	require.NoError(t, err)

	return spaceBinding
}

// CreateSpaceBinding creates SpaceBinding resource for the given MUR & Space with the given space role
func CreateSpaceBinding(t *testing.T, hostAwait *wait.HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord, space *toolchainv1alpha1.Space, spaceRole string) *toolchainv1alpha1.SpaceBinding {
	spaceBinding := NewSpaceBinding(mur, space, spaceRole)
	err := hostAwait.CreateWithCleanup(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return spaceBinding
}

// CreateSpaceBindingWithoutCleanup creates SpaceBinding resource for the given MUR & Space with the given space role; and doesn't mark the resource to be ready for cleanup
func CreateSpaceBindingWithoutCleanup(t *testing.T, hostAwait *wait.HostAwaitility, mur *toolchainv1alpha1.MasterUserRecord, space *toolchainv1alpha1.Space, spaceRole string) *toolchainv1alpha1.SpaceBinding {
	spaceBinding := NewSpaceBinding(mur, space, spaceRole)
	err := hostAwait.Client.Create(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return spaceBinding
}

// NewSpaceBinding create an object SpaceBinding with the given values
func NewSpaceBinding(mur *toolchainv1alpha1.MasterUserRecord, space *toolchainv1alpha1.Space, spaceRole string) *toolchainv1alpha1.SpaceBinding {
	namePrefix := fmt.Sprintf("%s-%s", mur.Name, space.Name)
	if len(namePrefix) > 50 {
		namePrefix = namePrefix[0:50]
	}
	return &toolchainv1alpha1.SpaceBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namePrefix + "-",
			Namespace:    space.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey: mur.Name,
				toolchainv1alpha1.SpaceBindingSpaceLabelKey:            space.Name,
			},
		},
		Spec: toolchainv1alpha1.SpaceBindingSpec{
			MasterUserRecord: mur.Name,
			Space:            space.Name,
			SpaceRole:        spaceRole,
		},
	}
}
