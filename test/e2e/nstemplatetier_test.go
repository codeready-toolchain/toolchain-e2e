package e2e

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/require"
)

func TestCreateOrUpdateNSTemplateTierAtStartup(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := doubles.InitializeOperators(t, tierList)
	defer ctx.Cleanup()
	hostAwait := wait.NewHostAwaitility(awaitility)

	// check the "advanced" NSTemplateTier, expecting generation=1 (just created)
	err := hostAwait.WaitForNSTemplateTier("advanced", wait.NSTemplateTierHavingGeneration(1))
	require.NoError(t, err)

	// check the "basic" NSTemplateTier, expecting generation=2 (just updated)
	err = hostAwait.WaitForNSTemplateTier("basic", wait.NSTemplateTierHavingGeneration(2))
	require.NoError(t, err)

}
