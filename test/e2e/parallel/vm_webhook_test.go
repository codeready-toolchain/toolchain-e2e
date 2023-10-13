package parallel

import (
	"context"
	"testing"

	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

func TestCreateVirtualMachine(t *testing.T) {
	// given
	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	client, err := dynamic.NewForConfig(memberAwait.RestConfig)
	if err != nil {
		panic(err)
	}

	// Provision a user to create the vm
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	NewSignupRequest(awaitilities).
		Username("test-vm").
		Email("test-vm@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	t.Run("create virtual machine", func(t *testing.T) {
		// given
		vmName := "test-vm"
		vmNamespace := "test-vm-dev"

		// when
		// create VM
		vmRes := schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}
		vm := vmResourceWithRequests(vmName, vmNamespace)
		_, err := client.Resource(vmRes).Namespace(vmNamespace).Create(context.TODO(), vm, metav1.CreateOptions{})

		// then
		// verify no error creating VM
		require.NoError(t, err)

		// verify webhook has set limits to same value as requests
		result, getErr := client.Resource(vmRes).Namespace(vmNamespace).Get(context.TODO(), vmName, metav1.GetOptions{})
		require.NoError(t, getErr)

		// verify requests are still set
		requests, found, requestsErr := unstructured.NestedStringMap(result.Object, "spec", "template", "spec", "domain", "resources", "requests")
		require.NoError(t, requestsErr)
		require.True(t, found)
		require.Equal(t, requests["memory"], "2Gi")
		require.Equal(t, requests["cpu"], "1")

		// verify limits are set
		limits, found, limitsErr := unstructured.NestedStringMap(result.Object, "spec", "template", "spec", "domain", "resources", "limits")
		require.NoError(t, limitsErr)
		require.True(t, found)
		require.Equal(t, limits["memory"], "2Gi")
		require.Equal(t, limits["cpu"], "1")
	})
}
func vmResourceWithRequests(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": name,
					},
					"spec": map[string]interface{}{
						"domain": map[string]interface{}{
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"memory": "2Gi",
									"cpu":    "1",
								},
							},
						},
					},
				},
			},
		},
	}
}
