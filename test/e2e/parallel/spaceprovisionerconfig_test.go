package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/assertions"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceProvisionerConfig(t *testing.T) {
	// given
	t.Parallel()

	awaitilities := WaitForDeployments(t)
	host := awaitilities.Host()

	t.Run("ready with existing cluster", func(t *testing.T) {
		// given
		// any ToolchainCluster in th host namespace will do. We don't really care...
		cluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)

		// when
		spc := Create(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		assert.NoError(t, err)
	})

	t.Run("not ready without existing cluster", func(t *testing.T) {
		// when
		spc := Create(t, host.Awaitility, ReferencingToolchainCluster("invalid%@@name"))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		assert.NoError(t, err)
	})

	t.Run("becomes ready when cluster appears", func(t *testing.T) {
		// given
		clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])

		// when
		spc := Create(t, host.Awaitility, ReferencingToolchainCluster(clusterName))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		assert.NoError(t, err)

		// when
		cluster := &toolchainv1alpha1.ToolchainCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: host.Namespace,
			},
		}
		assert.NoError(t, host.CreateWithCleanup(t, cluster))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		assert.NoError(t, err)
	})

	t.Run("becomes not ready when cluster disappears", func(t *testing.T) {
		// given
		clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
		cluster := &toolchainv1alpha1.ToolchainCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: host.Namespace,
			},
		}
		assert.NoError(t, host.CreateWithCleanup(t, cluster))

		// when
		spc := Create(t, host.Awaitility, ReferencingToolchainCluster(clusterName))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		assert.NoError(t, err)

		// when
		assert.NoError(t, host.Client.Delete(context.TODO(), cluster))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		assert.NoError(t, err)
	})
}
