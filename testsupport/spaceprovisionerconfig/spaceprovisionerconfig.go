package spaceprovisionerconfig

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

func CreateSpaceProvisionerConfig(t *testing.T, await *wait.Awaitility, opts ...testSpc.CreateOption) *toolchainv1alpha1.SpaceProvisionerConfig {
	namePrefix := util.NewObjectNamePrefix(t)

	spc := testSpc.NewSpaceProvisionerConfig("", await.Namespace, opts...)
	spc.GenerateName = namePrefix
	err := await.CreateWithCleanup(t, spc)
	require.NoError(t, err)

	return spc
}
