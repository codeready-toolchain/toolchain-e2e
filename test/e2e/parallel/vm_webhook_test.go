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
		vm := vmResourceWithRequests(vmName)
		_, err := client.Resource(vmRes).Namespace(vmNamespace).Create(context.TODO(), vm, metav1.CreateOptions{})

		// then
		// verify no error creating VM
		require.NoError(t, err)

		// verify webhook has set limits to same value as requests
		result, getErr := client.Resource(vmRes).Namespace(vmNamespace).Get(context.TODO(), vmName, metav1.GetOptions{})
		require.NoError(t, getErr)

		// verify requests are still set
		requests, requestsFound, requestsErr := unstructured.NestedStringMap(result.Object, "spec", "template", "spec", "domain", "resources", "requests")
		require.NoError(t, requestsErr)
		require.True(t, requestsFound)
		require.Equal(t, requests["memory"], "2Gi")
		require.Equal(t, requests["cpu"], "1")

		// verify limits are set
		limits, limitsFound, limitsErr := unstructured.NestedStringMap(result.Object, "spec", "template", "spec", "domain", "resources", "limits")
		require.NoError(t, limitsErr)
		require.True(t, limitsFound)
		require.Equal(t, limits["memory"], "2Gi")
		require.Equal(t, limits["cpu"], "1")

		// verify ssh key is set
		volumes, volumesFound, volumesErr := unstructured.NestedSlice(result.Object, "spec", "template", "spec", "volumes")
		require.NoError(t, volumesErr)
		require.True(t, volumesFound)
		require.Len(t, volumes, 1)
		volName, volNameExists := volumes[0].(map[string]interface{})["name"]
		require.True(t, volNameExists, "volume name not found")
		require.Equal(t, volName, "cloudinitdisk")

		userData, userDataFound, userDataErr := unstructured.NestedString(volumes[0].(map[string]interface{}), "cloudInitNoCloud", "userData")
		require.NoError(t, userDataErr)
		require.True(t, userDataFound, "user data not found")
		require.Equal(t, userData, "#cloud-config\nchpasswd:\n  expire: false\npassword: abcd-1234-ef56\nssh_authorized_keys:\n- |\n  ssh-rsa tmpkey human@machine\nuser: cloud-user\n")
	})
}
func vmResourceWithRequests(name string) *unstructured.Unstructured {
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
						"volumes": []map[string]interface{}{
							{
								"cloudInitNoCloud": map[string]interface{}{
									"userData": "#cloud-config\nchpasswd:\n  expire: false\npassword: abcd-1234-ef56\nuser: cloud-user\n",
								},
								"name": "cloudinitdisk",
							},
						},
					},
				},
			},
		},
	}
}
