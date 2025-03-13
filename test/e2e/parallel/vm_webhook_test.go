package parallel

import (
	"context"
	"fmt"
	"testing"

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

var vmRes = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}

func TestCreateVirtualMachine(t *testing.T) {
	// given
	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	memberAwait := awaitilities.Member1()

	client, err := dynamic.NewForConfig(memberAwait.RestConfig)
	if err != nil {
		panic(err)
	}

	userCounter := 1

	for tcname, tc := range map[string]struct {
		vmName              string
		domain              map[string]interface{}
		cloudInitType       string
		expectedMemoryLimit string
		expectedCPULimit    string
	}{
		"vm-cloudInitNoCloud-domain-resource-requests": {
			vmName:              "cloudinit-nocloud-domain-resources-req",
			domain:              domainWithResourceRequests("2Gi", "1"),
			cloudInitType:       cloudInitNoCloud,
			expectedMemoryLimit: "2Gi",
			expectedCPULimit:    "1",
		},
		"vm-cloudInitConfigDrive-domain-resource-requests": {
			vmName:              "cloudinit-configdrive-domain-resources-req",
			domain:              domainWithResourceRequests("3Gi", "2"),
			cloudInitType:       cloudInitConfigDrive,
			expectedMemoryLimit: "3Gi",
			expectedCPULimit:    "2",
		},
		"vm-domain-memory-guest": {
			vmName:              "domain-memory-guest",
			domain:              domainWithMemoryGuest("4Gi"),
			cloudInitType:       cloudInitNoCloud,
			expectedMemoryLimit: "4Gi",
			expectedCPULimit:    "",
		},
	} {
		t.Run(tcname, func(t *testing.T) {
			// create a user for each scenario to avoid vm quota limit
			username := fmt.Sprintf("test-vm-%d", userCounter)
			useremail := fmt.Sprintf("%s@redhat.com", username)
			vmNamespace := fmt.Sprintf("%s-dev", username)
			NewSignupRequest(awaitilities).
				Username(username).
				Email(useremail).
				ManuallyApprove().
				EnsureMUR().
				TargetCluster(memberAwait).
				RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
				Execute(t)

			userCounter++

			// given
			vm := vmResourceWithRequestsAndCloudInitVolume(tc.vmName, tc.domain, cloudInitVolume(tc.cloudInitType))

			// when
			// create VM
			_, err := client.Resource(vmRes).Namespace(vmNamespace).Create(context.TODO(), vm, metav1.CreateOptions{})

			// then
			// verify no error creating VM
			require.NoError(t, err)

			// verify webhook has mutated the VM
			result, getErr := client.Resource(vmRes).Namespace(vmNamespace).Get(context.TODO(), tc.vmName, metav1.GetOptions{})
			require.NoError(t, getErr)

			// verify limits are set to same value as requests
			limits, limitsFound, limitsErr := unstructured.NestedStringMap(result.Object, "spec", "template", "spec", "domain", "resources", "limits")
			require.NoError(t, limitsErr)
			require.True(t, limitsFound)
			require.Equal(t, tc.expectedMemoryLimit, limits["memory"])
			require.Equal(t, tc.expectedCPULimit, limits["cpu"])

			// verify volume exists
			volumes, volumesFound, volumesErr := unstructured.NestedSlice(result.Object, "spec", "template", "spec", "volumes")
			require.NoError(t, volumesErr)
			require.True(t, volumesFound)
			require.Len(t, volumes, 1)
			volName, volNameExists := volumes[0].(map[string]interface{})["name"]
			require.True(t, volNameExists, "volume name not found")
			require.Equal(t, "cloudinitdisk", volName)

			// verify sandbox toleration is set
			tolerations, tolerationsFound, tolerationsErr := unstructured.NestedSlice(result.Object, "spec", "template", "spec", "tolerations")
			require.NoError(t, tolerationsErr)
			require.True(t, tolerationsFound)
			require.Len(t, tolerations, 1)
			tol, ok := tolerations[0].(map[string]interface{})
			require.True(t, ok)
			require.Equal(t, "NoSchedule", tol["effect"])
			require.Equal(t, "sandbox-cnv", tol["key"])
			require.Equal(t, "Exists", tol["operator"])

			// verify cloud-init user data
			userData, userDataFound, userDataErr := unstructured.NestedString(volumes[0].(map[string]interface{}), tc.cloudInitType, "userData")
			require.NoError(t, userDataErr)
			require.True(t, userDataFound, "user data not found")
			require.Equal(t, "#cloud-config\nchpasswd:\n  expire: false\npassword: abcd-1234-ef56\nssh_authorized_keys:\n- |\n  ssh-rsa PcHUNFXhysGvTnvORVbR70EVZA test@host-operator\nuser: cloud-user\n", userData)

			// delete VM after verifying to avoid hitting VM quota limit
			err = client.Resource(vmRes).Namespace(vmNamespace).Delete(context.TODO(), tc.vmName, metav1.DeleteOptions{})
			require.NoError(t, err)
		})
	}
}

func vmResourceWithRequestsAndCloudInitVolume(name string, domain map[string]interface{}, volume map[string]interface{}) *unstructured.Unstructured {
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
						"domain": domain,
						"volumes": []map[string]interface{}{
							volume,
						},
					},
				},
			},
		},
	}
}

func domainWithMemoryGuest(mem string) map[string]interface{} {
	return map[string]interface{}{
		"memory": map[string]interface{}{
			"guest": mem,
		},
		"devices": map[string]interface{}{},
	}
}

func domainWithResourceRequests(mem, cpu string) map[string]interface{} {
	return map[string]interface{}{
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{
				"memory": mem,
				"cpu":    cpu,
			},
		},
		"devices": map[string]interface{}{},
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
