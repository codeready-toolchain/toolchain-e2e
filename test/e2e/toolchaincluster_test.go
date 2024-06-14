package e2e

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestToolchainClusterE2E(t *testing.T) {
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	hostAwait.WaitForToolchainClusterResources(t)
	memberAwait := awaitilities.Member1()
	memberAwait.WaitForToolchainClusterResources(t)

	// NOTE: because we cannot introduce changes to host operator and member operator simulateneously to enforce
	// backwards compatibility, we first introduce the conversion of connection details into kubeconfig to the host
	// operator. This is fine, because that data is not actually used for antyhing at the moment. Once this works
	// in the host operator and we merge it, it can also be introduced into the member operator (and this test changed)
	// and we can move forward with secrets updated everywhere.
	verifyToolchainCluster(t, hostAwait.Awaitility, memberAwait.Awaitility, true)
	verifyToolchainCluster(t, memberAwait.Awaitility, hostAwait.Awaitility, false)
}

// verifyToolchainCluster verifies existence and correct conditions of ToolchainCluster CRD
// in the target cluster type operator
func verifyToolchainCluster(t *testing.T, await *wait.Awaitility, otherAwait *wait.Awaitility, kubeConfigShouldExist bool) {
	// given
	current, ok, err := await.GetToolchainCluster(t, otherAwait.Namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)
	require.True(t, ok, "ToolchainCluster should exist")

	// NOTE: this needs to run first, before the sub-tests below, because they reuse the secret for the new
	// toolchain clusters. This is technically incorrect but sufficient for those tests.
	// Note that we are going to be changing the workflow such that the label on the secret will actually be the driver
	// for ToolchainCluster creation and so re-using the secret for different TCs will become impossible in the future.
	t.Run("referenced secret is labeled", func(t *testing.T) {
		secret := corev1.Secret{}
		require.NoError(t, await.Client.Get(context.TODO(), client.ObjectKey{Name: current.Spec.SecretRef.Name, Namespace: current.Namespace}, &secret))

		require.Equal(t, current.Name, secret.Labels[toolchainv1alpha1.ToolchainClusterLabel], "the secret of the ToolchainCluster %s is not labeled", client.ObjectKeyFromObject(&current))
	})

	// NOTE: this checks that there is the migration step in place that converts the connection details for the cluster that is currently
	// spread over the secret and ToolchainCluster is also set as a kubeconfig inside the secret. Once we migrate to create the ToolchainClusters
	// based on the secrets, we will no longer require this migration step and this test will be removed.
	t.Run("kubeconfig is generated from the connection details", func(t *testing.T) {
		secret := &corev1.Secret{}
		require.NoError(t, await.Client.Get(context.TODO(), client.ObjectKey{Name: current.Spec.SecretRef.Name, Namespace: current.Namespace}, secret))

		// this is enough for the test here. The correctness of the contained kubeconfig is checked in the unit tests of the controller in toolchain-common.
		_, present := secret.Data["kubeconfig"]
		assert.Equal(t, kubeConfigShouldExist, present)
	})

	t.Run(fmt.Sprintf("create new ToolchainCluster based on '%s' with correct data and expect to be ready", current.Name), func(t *testing.T) {
		// given
		name := generateNewName("new-ready-", current.Name)
		secretCopy := &corev1.Secret{}
		wait.CopyWithCleanup(t, await,
			client.ObjectKey{Name: current.Spec.SecretRef.Name, Namespace: current.Namespace},
			client.ObjectKey{Name: name, Namespace: current.Namespace},
			secretCopy)

		toolchainCluster := newToolchainCluster(await.Namespace, name,
			apiEndpoint(current.Spec.APIEndpoint),
			caBundle(current.Spec.CABundle),
			secretRef(secretCopy.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)

		// when
		err = await.CreateWithCleanup(t, toolchainCluster)
		require.NoError(t, err)
		// wait for toolchaincontroller to reconcile
		time.Sleep(1 * time.Second)

		// then the ToolchainCluster should be ready
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasName(toolchainCluster.Name),
			wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ConditionReady),
		)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(
				client.MatchingLabels{
					"namespace": otherAwait.Namespace,
				},
			), wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ConditionReady),
		)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(client.MatchingLabels{
				"namespace": await.Namespace,
			}), wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ConditionReady),
		)
		require.NoError(t, err)
	})

	t.Run(fmt.Sprintf("create new ToolchainCluster based on '%s' with incorrect data and expect to be offline", current.Name), func(t *testing.T) {
		// given
		name := generateNewName("new-offline-", current.Name)
		secretCopy := &corev1.Secret{}
		wait.CopyWithCleanup(t, await,
			client.ObjectKey{Name: current.Spec.SecretRef.Name, Namespace: current.Namespace},
			client.ObjectKey{Name: name, Namespace: current.Namespace}, secretCopy)

		toolchainCluster := newToolchainCluster(await.Namespace, name,
			apiEndpoint("https://1.2.3.4:8443"),
			caBundle(current.Spec.CABundle),
			secretRef(secretCopy.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)

		// when
		err := await.CreateWithCleanup(t, toolchainCluster)
		// wait for toolchaincontroller to reconcile
		time.Sleep(1 * time.Second)

		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasName(toolchainCluster.Name),
			wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ToolchainClusterOffline),
		)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(
				client.MatchingLabels{
					"namespace": otherAwait.Namespace,
				},
			), wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ConditionReady),
		)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(client.MatchingLabels{
				"namespace": await.Namespace,
			}), wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ConditionReady),
		)
		require.NoError(t, err)
	})
}

func generateNewName(prefix, baseName string) string {
	name := prefix + baseName
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

func newToolchainCluster(namespace, name string, options ...clusterOption) *toolchainv1alpha1.ToolchainCluster {
	toolchainCluster := &toolchainv1alpha1.ToolchainCluster{
		Spec: toolchainv1alpha1.ToolchainClusterSpec{
			SecretRef: toolchainv1alpha1.LocalSecretReference{
				Name: "", // default
			},
			APIEndpoint: "", // default
			CABundle:    "", // default
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"ownerClusterName": "east",
			},
		},
	}
	for _, configure := range options {
		configure(toolchainCluster)
	}
	return toolchainCluster
}

// clusterOption an option to configure the cluster to use in the tests
type clusterOption func(*toolchainv1alpha1.ToolchainCluster)

// capacityExhausted an option to state that the cluster capacity has exhausted
var capacityExhausted clusterOption = func(c *toolchainv1alpha1.ToolchainCluster) {
	c.Labels["toolchain.dev.openshift.com/capacity-exhausted"] = strconv.FormatBool(true)
}

// Owner sets the 'ownerClusterName' label
func owner(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["ownerClusterName"] = name
	}
}

// Namespace sets the 'namespace' label
func namespace(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["namespace"] = name
	}
}

// SecretRef sets the SecretRef in the cluster's Spec
func secretRef(ref string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.SecretRef = toolchainv1alpha1.LocalSecretReference{
			Name: ref,
		}
	}
}

// APIEndpoint sets the APIEndpoint in the cluster's Spec
func apiEndpoint(url string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.APIEndpoint = url
	}
}

// CABundle sets the CABundle in the cluster's Spec
func caBundle(bundle string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.CABundle = bundle
	}
}
