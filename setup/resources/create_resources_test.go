package resources

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	commontest "github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateFromTemplateFile(t *testing.T) {
	// given
	s, err := configuration.NewScheme()
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		// given
		t.Cleanup(func() {
			tmpl = nil // forget about the template after this test, so others can fail as expected
		})
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "user0001-stage",
			},
		}
		cl := commontest.NewFakeClient(t, ns)
		username := "user0001"
		templatePath := "user-workloads.yaml"

		// when
		err := CreateFromTemplateFile(cl, s, templatePath, username)

		// then
		require.NoError(t, err)
		assert.NoError(t, cl.Get(context.TODO(),
			types.NamespacedName{
				Namespace: "user0001-stage",
				Name:      "nginx-deployment",
			},
			&appsv1.Deployment{}))
		assert.NoError(t, cl.Get(context.TODO(),
			types.NamespacedName{
				Namespace: "user0001-stage",
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
				err := CreateFromTemplateFile(cl, s, templatePath, username)

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
				tmpFile, err := ioutil.TempFile(os.TempDir(), "setup-template-")
				require.NoError(t, err)
				tmpFile.WriteString(deployment)

				// when
				err = CreateFromTemplateFile(cl, s, tmpFile.Name(), username)

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
