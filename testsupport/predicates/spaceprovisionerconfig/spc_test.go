package spaceprovisionerconfig

import (
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates/conditions"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestThat(t *testing.T) {
	test := That().
		HasName("expected").
		HasConditionWithType(v1alpha1.ConditionReady, conditions.WithStatus(corev1.ConditionTrue)).
		ReferencesToolchainCluster("cluster-1").
		HasFinalizer("fin")

	preds := test.Predicates()
	assert.Len(t, preds, 4)

	spc := &v1alpha1.SpaceProvisionerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "actual",
		},
		Spec: v1alpha1.SpaceProvisionerConfigSpec{
			ToolchainCluster: "cluster-2",
		},
		Status: v1alpha1.SpaceProvisionerConfigStatus{
			Conditions: []v1alpha1.Condition{
				{
					Type:   v1alpha1.ConditionReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	// not actual tests here... This is just to visually inspect the test output
	// to check that the diff actually works in Explain... This should of course
	// be properly tested if we go with this approach.

	expl, _ := predicates.Explain(spc, preds[0])
	assert.Equal(t, "this is not the actual output", expl)

	expl, _ = predicates.Explain(spc, preds[1])
	assert.Equal(t, "this is not the actual output", expl)

	expl, _ = predicates.Explain(spc, preds[2])
	assert.Equal(t, "this is not the actual output", expl)

	expl, _ = predicates.Explain(spc, preds[3])
	assert.Equal(t, "this is not the actual output", expl)
}
