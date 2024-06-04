package parallel

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/assertions"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
		// any ToolchainCluster in th host namespace will do. We don't really care...
		cluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
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
	t.Run("becomes ready when cluster becomes ready", func(t *testing.T) {
		// given
		existingCluster, err := host.WaitForToolchainCluster(t, wait.UntilToolchainClusterHasName(awaitilities.Member1().ClusterName))
		require.NoError(t, err)
		tc := copyClusterWithoutSecret(t, host.Awaitility, existingCluster)
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(tc.Name))

		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)

		existingSecret := &corev1.Secret{}
		require.NoError(t, host.Client.Get(context.TODO(), client.ObjectKey{Name: existingCluster.Spec.SecretRef.Name, Namespace: existingCluster.Namespace}, existingSecret))

		newSecretName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
		_ = copySecret(t, host.Awaitility, existingSecret, client.ObjectKey{Name: newSecretName, Namespace: tc.Namespace})

		// when

		// we need to retry here because we're fighting over the control of the TC with the controller
		require.NoError(t, kwait.Poll(100*time.Millisecond, 1*time.Minute, func() (done bool, err error) {
			err = host.Client.Get(context.TODO(), client.ObjectKeyFromObject(tc), tc)
			if err != nil {
				return
			}
			tc.Spec.SecretRef.Name = newSecretName
			err = host.Client.Update(context.TODO(), tc)
			if err != nil {
				return
			}
			done = true
			return
		}))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		require.NoError(t, err)
	})
	t.Run("becomes not ready when cluster disappears", func(t *testing.T) {
		// given

		// we need to create a copy of the cluster and the token secret
		existingCluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)
		cluster := copyClusterWithSecret(t, host.Awaitility, existingCluster)

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		require.NoError(t, err)

		// when
		assert.NoError(t, host.Client.Delete(context.TODO(), cluster))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)
	})
}

func copyClusterWithSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	t.Helper()
	clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])

	// copy the secret
	secret := &corev1.Secret{}
	require.NoError(t, a.CopyWithCleanup(t,
		client.ObjectKey{
			Name:      cluster.Spec.SecretRef.Name,
			Namespace: cluster.Namespace,
		},
		client.ObjectKey{
			Name:      clusterName,
			Namespace: cluster.Namespace,
		},
		secret,
	))

	// and create a new ToolchainCluster with that secret
	// note that we can't use the CopyWithCleanup function because we also
	// need to modify the SecretRef of the TC.
	newCluster := cluster.DeepCopy()
	newCluster.ResourceVersion = ""
	newCluster.UID = ""
	newCluster.Name = clusterName
	newCluster.Spec.SecretRef.Name = secret.Name
	newCluster.Status = toolchainv1alpha1.ToolchainClusterStatus{}
	require.NoError(t, a.CreateWithCleanup(t, newCluster))

	return newCluster
}

func copyClusterWithoutSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	t.Helper()
	newName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
	newCluster := cluster.DeepCopy()
	newCluster.ResourceVersion = ""
	newCluster.UID = ""
	newCluster.Name = newName
	newCluster.Spec.SecretRef.Name = ""
	newCluster.Status = toolchainv1alpha1.ToolchainClusterStatus{}
	require.NoError(t, a.CreateWithCleanup(t, newCluster))

	return newCluster
}

func copySecret(t *testing.T, a *wait.Awaitility, source *corev1.Secret, targetKey client.ObjectKey) *corev1.Secret {
	t.Helper()
	target := source.DeepCopy()
	target.ResourceVersion = ""
	target.UID = ""
	target.Name = targetKey.Name
	target.Namespace = targetKey.Namespace
	require.NoError(t, a.CreateWithCleanup(t, target))
	return target
}
