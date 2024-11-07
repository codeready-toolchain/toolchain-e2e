package e2e

import (
	"context"
	"testing"

	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestToolchainConfig(t *testing.T) {
	awaitilities := testsupport.WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	t.Run("update replica count for registration-service", func(t *testing.T) {
		// get current replica count in registration-service
		registrationServiceName := "registration-service"
		rs := &appsv1.Deployment{}
		err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: hostAwait.Namespace, Name: registrationServiceName}, rs)
		require.NoError(t, err)
		currentNrReplicas := *rs.Spec.Replicas

		// update replica count in registration-service
		newNrReplicas := currentNrReplicas + 1
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Replicas(newNrReplicas))
		hostAwait.WaitForDeploymentToGetReady(t, registrationServiceName, int(newNrReplicas))

		// no need to revert to original replica count
		// UpdateToolchainConfig returns the resource back to the original value/state at the end of the test
	})
}
