package spaceprovisionerconfig

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateSpaceProvisionerConfig(t *testing.T, await *wait.Awaitility, opts ...testSpc.CreateOption) *toolchainv1alpha1.SpaceProvisionerConfig {
	namePrefix := util.NewObjectNamePrefix(t)

	spc := testSpc.NewSpaceProvisionerConfig("", await.Namespace, opts...)
	spc.GenerateName = namePrefix
	err := await.CreateWithCleanup(t, spc)
	require.NoError(t, err)

	return spc
}

func UpdateForCluster(t *testing.T, await *wait.Awaitility, referencedClusterName string, opts ...testSpc.CreateOption) {
	t.Helper()
	spcs, err := getAllSpcs(t, await)
	require.NoError(t, err)

	idx := findIndexOfFirstSpaceProvisionerConfigReferencingCluster(spcs, referencedClusterName)
	require.GreaterOrEqual(t, idx, 0, "could not find SpaceProvisionerConfig referencing the required cluster: %s", referencedClusterName)

	spc := &spcs[idx]

	for _, opt := range opts {
		opt(spc)
	}

	assert.NoError(t, await.Client.Update(context.TODO(), spc))
}

func getAllSpcs(t *testing.T, await *wait.Awaitility) ([]toolchainv1alpha1.SpaceProvisionerConfig, error) {
	t.Helper()
	list := &toolchainv1alpha1.SpaceProvisionerConfigList{}
	if err := await.Client.List(context.TODO(), list, client.InNamespace(await.Namespace)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func findIndexOfFirstSpaceProvisionerConfigReferencingCluster(spcs []toolchainv1alpha1.SpaceProvisionerConfig, clusterName string) int {
	for i, spc := range spcs {
		if spc.Spec.ToolchainCluster == clusterName {
			return i
		}
	}
	return -1
}
