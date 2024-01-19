package spaceprovisionerconfig

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type (
	CreateOption func(*toolchainv1alpha1.SpaceProvisionerConfig)
	predicate    func(*toolchainv1alpha1.SpaceProvisionerConfig) bool
)

var _ predicates.Predicate[*toolchainv1alpha1.SpaceProvisionerConfig] = (predicate)(nil)

func (f predicate) Matches(spc *toolchainv1alpha1.SpaceProvisionerConfig) bool {
	return f(spc)
}

func ReferencingToolchainCluster(name string) CreateOption {
	return func(spc *toolchainv1alpha1.SpaceProvisionerConfig) {
		spc.Spec.ToolchainCluster = name
	}
}

func Ready() predicates.Predicate[*toolchainv1alpha1.SpaceProvisionerConfig] {
	// Go's type inference is a bit lacking so we have to help it realize that we want
	// to use the function as a predicate
	var f predicate = func(spc *toolchainv1alpha1.SpaceProvisionerConfig) bool {
		return condition.IsTrue(spc.Status.Conditions, toolchainv1alpha1.ConditionReady)
	}
	return f
}

func NotReady() predicates.Predicate[*toolchainv1alpha1.SpaceProvisionerConfig] {
	// Go's type inference is a bit lacking so we have to help it realize that we want
	// to use the function as a predicate
	var f predicate = func(spc *toolchainv1alpha1.SpaceProvisionerConfig) bool {
		return condition.IsFalse(spc.Status.Conditions, toolchainv1alpha1.ConditionReady)
	}
	return f
}

func CreateSpaceProvisionerConfig(t *testing.T, await *wait.Awaitility, opts ...CreateOption) *toolchainv1alpha1.SpaceProvisionerConfig {
	namePrefix := util.NewObjectNamePrefix(t)

	spc := &toolchainv1alpha1.SpaceProvisionerConfig{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namePrefix + "-",
			Namespace:    await.Namespace,
		},
	}
	for _, apply := range opts {
		apply(spc)
	}
	err := await.CreateWithCleanup(t, spc)
	require.NoError(t, err)

	return spc
}
