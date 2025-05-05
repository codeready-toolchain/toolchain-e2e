package assertions_use

import (
	"context"
	"testing"

	toolchainv1aplha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/conditions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/assertions/metadata"
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

	// use the assertions in a simple immediate call
	spaceprovisionerconfig.Asserting().
		ObjectMeta(metadata.With().Name("asdf").Label("kachny")).
		Conditions(conditions.With().Type(toolchainv1aplha1.ConditionReady)).
		Test(t, spcUnderTest)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(spcUnderTest).Build()

	// this is the new WaitFor
	spaceprovisionerconfig.Asserting().
		ObjectMeta(metadata.With().Name("asdf").Namespace("default").Label("kachny")).
		Conditions(conditions.With().Type(toolchainv1aplha1.ConditionReady)).
		Await(cl).
		Matching(context.TODO(), t)
}
