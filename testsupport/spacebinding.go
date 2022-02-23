package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
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
