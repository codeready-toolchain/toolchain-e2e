package assertions_use

import (
	"context"
	"testing"
	"time"

	toolchainv1aplha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/spaceprovisionerconfig"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test(t *testing.T) {
	spcUnderTest := &toolchainv1aplha1.SpaceProvisionerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kachny",
			Namespace: "default",
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, toolchainv1aplha1.AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(spcUnderTest).Build()

	// use the assertions in a simple immediate call
	assertions.Test(t, spcUnderTest, spaceprovisionerconfig.That().HasLabel("asdf").HasConditionWithType(toolchainv1aplha1.ConditionReady))

	// this is the new WaitFor
	assertions.WaitFor[*toolchainv1aplha1.SpaceProvisionerConfig](cl).
		WithTimeout(1*time.Second). // defaults to wait.DefaultTimeout which is 2 minutes, so let's make it shorter here
		WithObjectKey("default", "kachny").
		Matching(context.TODO(), t,
			spaceprovisionerconfig.That().HasLabel("asdf"))
}
