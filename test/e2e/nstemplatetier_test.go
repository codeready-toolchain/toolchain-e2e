package e2e

import (
	"context"
	"os"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateNSTemplateTierAtStartup(t *testing.T) {
	// given:
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := doubles.InitializeOperators(t, tierList)
	defer ctx.Cleanup()
	hostNs := os.Getenv(wait.HostNsVar)

	// when
	err := awaitility.Client.List(context.TODO(), client.InNamespace(hostNs), tierList)

	// then
	require.NoError(t, err)
	assert.Len(t, tierList.Items, 2)
	for _, tier := range tierList.Items {
		switch tier.Name {
		case "advanced":
			// the `advanced` tier should be in generation 1 (just created)
			assert.Equal(t, int64(1), tier.ObjectMeta.GetGeneration())
		case "basic":
			// the `basic` tier should be in generation 1 (just updated - was created by makefile)
			assert.Equal(t, int64(2), tier.ObjectMeta.GetGeneration())
		default:
			t.Fatalf("unexpected tier: %s", tier.Name)
		}
	}

}
