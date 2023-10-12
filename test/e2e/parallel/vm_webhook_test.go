package parallel

import (
	"testing"

	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	vmapi "github.com/codeready-toolchain/toolchain-common/pkg/virtualmachine/api"
	"github.com/stretchr/testify/require"

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
		err := createVM(t, memberAwait, vmName, vmNamespace, vmapi.WithRequests(vmapi.ResourceList("1", "2Gi"))) // only requests are set, no limits set

		// then

		// verify no error creating VM
		require.NoError(t, err)

		// verify webhook has set limits to same value as requests
		_, err = memberAwait.WaitForVM(t, vmName, vmNamespace, wait.UntilVMHasRequests(vmapi.ResourceList("1", "2Gi")), wait.UntilVMHasLimits(vmapi.ResourceList("1", "2Gi")))
		require.NoError(t, err)
	})
}

func createVM(t *testing.T, memberAwait *wait.MemberAwaitility, name, namespace string, options ...vmapi.VMOption) error {
	vm := vmapi.NewVM(name, namespace, options...)
	return memberAwait.Create(t, vm)
}
