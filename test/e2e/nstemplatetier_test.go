package e2e

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/require"
)

func TestCreateOrUpdateNSTemplateTierAtStartup(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	hostAwait := NewHostAwaitility(awaitility)

	// check the "advanced" NSTemplateTier exists (just created)
	_, err := hostAwait.WaitForNSTemplateTier("advanced")
	require.NoError(t, err)

	// check the "team" NSTemplateTier exists (just created)
	_, err = hostAwait.WaitForNSTemplateTier("team")
	require.NoError(t, err)

	// check the "basic" NSTemplateTier exists, and all its Namespace revisions are different from `000000a`,
	// which is the value specified in the initial manifest
	_, err = hostAwait.WaitForNSTemplateTier("basic", UntilNSTemplateTierSpec(Not(HasNamespaceRevisions("000000a"))))
	require.NoError(t, err)
}
