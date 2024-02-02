package parallel

import (
	"context"
	"fmt"
	"strings"
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

const cloudInitNoCloud = "cloudInitNoCloud"
const cloudInitConfigDrive = "cloudInitConfigDrive"

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
		vmNamespace := "test-vm-dev"
		vmRes := schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}

		for _, cloudInitType := range []string{cloudInitNoCloud, cloudInitConfigDrive} {
			vmName := fmt.Sprintf("test-vm-%s", strings.ToLower(cloudInitType))
			t.Run(cloudInitType, func(t *testing.T) {
				vm := vmResourceWithRequestsAndCloudInitVolume(vmName, cloudInitVolume(cloudInitType))

				// when
				// create VM
				_, err := client.Resource(vmRes).Namespace(vmNamespace).Create(context.TODO(), vm, metav1.CreateOptions{})

				// then
				// verify no error creating VM
				require.NoError(t, err)

				// cleanup
				t.Cleanup(func() {
					err := client.Resource(vmRes).Namespace(vmNamespace).Delete(context.TODO(), vmName, metav1.DeleteOptions{})
					require.NoError(t, err)
				})

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

				// verify volume
				volumes, volumesFound, volumesErr := unstructured.NestedSlice(result.Object, "spec", "template", "spec", "volumes")
				require.NoError(t, volumesErr)
				require.True(t, volumesFound)
				require.Len(t, volumes, 1)
				volName, volNameExists := volumes[0].(map[string]interface{})["name"]
				require.True(t, volNameExists, "volume name not found")
				require.Equal(t, volName, "cloudinitdisk")

				// verify cloud-init user data
				userData, userDataFound, userDataErr := unstructured.NestedString(volumes[0].(map[string]interface{}), cloudInitType, "userData")
				require.NoError(t, userDataErr)
				require.True(t, userDataFound, "user data not found")
				require.Equal(t, userData, "#cloud-config\nchpasswd:\n  expire: false\npassword: abcd-1234-ef56\nssh_authorized_keys:\n- |\n  ssh-rsa PcHUNFXhysGvTnvORVbR70EVZA test@host-operator\nuser: cloud-user\n")
			})
		}
	})

}
func vmResourceWithRequestsAndCloudInitVolume(name string, volume map[string]interface{}) *unstructured.Unstructured {
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
							volume,
						},
					},
				},
			},
		},
	}
}

func cloudInitVolume(cloudInitType string) map[string]interface{} {
	return map[string]interface{}{
		cloudInitType: map[string]interface{}{
			"userData": "#cloud-config\nchpasswd:\n  expire: false\npassword: abcd-1234-ef56\nuser: cloud-user\n",
		},
		"name": "cloudinitdisk",
	}
}
