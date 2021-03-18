package users

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	commontest "github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreate(t *testing.T) {

	// given
	configuration.DefaultTimeout = time.Second * 5

	t.Run("success", func(t *testing.T) {
		// given
		// needed otherwise the following tests can't verify what happens when `memberClusterName` cannot be retrieved
		t.Cleanup(func() {
			memberClusterName = ""
		})
		hostOperatorNamespace := "toolchain-host-operator"
		memberOperatorNamespace := "toolchain-member-operator"
		memberCluster := &toolchainv1alpha1.ToolchainCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hostOperatorNamespace,
				Name:      "member-abcd",
				Labels: map[string]string{
					"namespace": memberOperatorNamespace,
					"type":      "member",
				},
			},
			Status: toolchainv1alpha1.ToolchainClusterStatus{
				Conditions: []toolchainv1alpha1.ToolchainClusterCondition{
					{
						Type:   toolchainv1alpha1.ToolchainClusterReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		cl := commontest.NewFakeClient(t, memberCluster)
		username := "user-0001"

		// when
		err := Create(cl, username, hostOperatorNamespace, memberOperatorNamespace)

		// then
		require.NoError(t, err)

	})

	t.Run("failures", func(t *testing.T) {

		t.Run("missing ToolchainCluster resource for member cluster", func(t *testing.T) {
			// given
			configuration.DefaultTimeout = time.Second * 5
			cl := commontest.NewFakeClient(t) // no ToolchainCluster
			username := "user-0001"
			hostOperatorNamespace := "toolchain-host-operator"
			memberOperatorNamespace := "toolchain-member-operator"

			// when
			err := Create(cl, username, hostOperatorNamespace, memberOperatorNamespace)

			// then
			require.Error(t, err)
			assert.EqualError(t, err, "unable to lookup member cluster name, ensure the sandbox setup steps are followed")
		})
	})
}
