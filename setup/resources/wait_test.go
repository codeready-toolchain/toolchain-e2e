package resources_test

import (
	"testing"
	"time"

	commontest "github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/resources"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWaitForNamespace(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		// given
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "user0001-stage",
			},
		}
		cl := commontest.NewFakeClient(t, ns) // ns exists

		// when
		err := resources.WaitForNamespace(cl, "user0001-stage")

		// then
		require.NoError(t, err)
	})

	t.Run("failures", func(t *testing.T) {

		t.Run("timeout", func(t *testing.T) {
			// given
			configuration.DefaultTimeout = time.Second * 1
			cl := commontest.NewFakeClient(t) // ns doesn't exist

			// when
			err := resources.WaitForNamespace(cl, "user0001-missing")

			// then
			require.Error(t, err)
			assert.EqualError(t, err, "namespace 'user0001-missing' does not exist: timed out waiting for the condition")
		})

	})
}
