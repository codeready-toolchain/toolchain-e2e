package resources

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commontest "github.com/codeready-toolchain/toolchain-common/pkg/test"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	templatev1 "github.com/openshift/api/template/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateUserResourcesFromTemplateFiles(t *testing.T) {
	// given
	configuration.DefaultTimeout = time.Millisecond * 1
	s, err := configuration.NewScheme()
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		// given
		t.Cleanup(func() {
			tmpls = make(map[string]*templatev1.Template) // forget about the template after this test, so others can fail as expected
		})
		space := testspace.NewSpace(configuration.HostOperatorNamespace, "user0001", testspace.WithCondition(
			toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
				Reason: "Provisioned",
			}))
		cl := commontest.NewFakeClient(t, space)
		username := "user0001"
		templatePath := "user-workloads.yaml"

		// when
		err := CreateUserResourcesFromTemplateFiles(context.TODO(), cl, s, username, []string{templatePath})

		// then
		require.NoError(t, err)
		assert.NoError(t, cl.Get(context.TODO(),
			types.NamespacedName{
				Namespace: "user0001-dev",
				Name:      "nginx-deployment",
			},
			&appsv1.Deployment{}))
		assert.NoError(t, cl.Get(context.TODO(),
			types.NamespacedName{
				Namespace: "user0001-dev",
				Name:      "nginx-service",
			},
			&corev1.Service{}))
	})

	t.Run("failures", func(t *testing.T) {
		t.Run("invalid template", func(t *testing.T) {
			t.Run("file not found", func(t *testing.T) {
				// given
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "user0001-stage",
					},
				}
				cl := commontest.NewFakeClient(t, ns)
				username := "user0001"
				templatePath := "not-found.yaml"

				// when
				err := CreateUserResourcesFromTemplateFiles(context.TODO(), cl, s, username, []string{templatePath})

				// then
				require.Error(t, err)
				assert.EqualError(t, err, "invalid template file: 'not-found.yaml': open not-found.yaml: no such file or directory")
			})

			t.Run("invalid content", func(t *testing.T) {
				// given
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "user0001-stage",
					},
				}
				cl := commontest.NewFakeClient(t, ns)
				username := "user0001"
				tmpFile, err := os.CreateTemp(os.TempDir(), "setup-template-")
				require.NoError(t, err)
				_, _ = tmpFile.WriteString(deployment)

				// when
				err = CreateUserResourcesFromTemplateFiles(context.TODO(), cl, s, username, []string{tmpFile.Name()})

				// then
				require.Error(t, err)
				assert.EqualError(t, err, fmt.Sprintf("invalid template file: '%s': wrong kind of object in the template file: 'apps/v1, Kind=Deployment'", tmpFile.Name()))
			})
		})
	})
}

const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  labels:
    app: nginx
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: quay.io/bitnami/nginx
        resources:
          requests:
            cpu: 250m
            memory: 64Mi
          limits:
            cpu: 500m
            memory: 250Mi
        ports:
        - containerPort: 80`
