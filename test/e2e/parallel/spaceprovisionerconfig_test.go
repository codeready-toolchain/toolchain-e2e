package parallel

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/assertions"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceProvisionerConfig(t *testing.T) {
	// given
	t.Parallel()

	awaitilities := WaitForDeployments(t)
	host := awaitilities.Host()

	t.Run("ready with existing ready cluster", func(t *testing.T) {
		// given
		// any ToolchainCluster in th host namespace will do. We don't really care, because both member1 and member2 should be ready...
		cluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)

		// when
		_, err = wait.For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).FirstThat(Has(ReferenceToToolchainCluster(cluster.Name)), Is(Ready()))

		// then
		require.NoError(t, err)
	})

	t.Run("not ready without existing cluster", func(t *testing.T) {
		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster("invalid%@@name"))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)
	})
	t.Run("contains the consumed capacity", func(t *testing.T) {
		test := func(t *testing.T, memberName string) {
			_, err := host.WaitForToolchainCluster(t, wait.UntilToolchainClusterHasName(memberName))
			require.NoError(t, err)

			spc, err := wait.For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).FirstThat(Has(ReferenceToToolchainCluster(memberName)))
			require.NoError(t, err)

			assert.NotNil(t, spc.Status.ConsumedCapacity)
			// we can't really test much about the actual values in the consumed capacity because this is a parallel test and the surrounding tests
			// may mess with the number of spaces and memory usage.
		}

		t.Run("for member1", func(t *testing.T) {
			test(t, awaitilities.Member1().ClusterName)
		})
		t.Run("for member2", func(t *testing.T) {
			test(t, awaitilities.Member2().ClusterName)
		})
	})
}
