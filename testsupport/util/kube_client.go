package util

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const EnvDisableKubeClientTLSVerify string = "DISABLE_KUBE_CLIENT_TLS_VERIFY"

func BuildKubernetesRESTConfig(apiConfig api.Config) (*rest.Config, error) {
	if os.Getenv(EnvDisableKubeClientTLSVerify) == "true" {
		apiConfig = setInsecureSkipTLSVerify(apiConfig)
	}

	configOverrides := clientcmd.ConfigOverrides{}
	return clientcmd.NewDefaultClientConfig(apiConfig, &configOverrides).ClientConfig()
}

// NewKubeClientFromSecret reads the kubeconfig from a given secret and create a kube rest client from it. You can supply functions to initialize
// the scheme with which the client will be built.
func NewKubeClientFromSecret(t *testing.T, cl client.Client, secretName, secretNamespace string, schemeAdders ...func(*runtime.Scheme) error) (client.Client, *corev1.Secret) {
	t.Helper()
	adminSecret := &corev1.Secret{}
	// retrieve the secret containing the kubeconfig
	require.NoError(t, cl.Get(context.TODO(), types.NamespacedName{
		Namespace: secretNamespace,
		Name:      secretName,
	}, adminSecret))
	assert.NotEmpty(t, adminSecret.Data["kubeconfig"])
	apiConfig, err := clientcmd.Load(adminSecret.Data["kubeconfig"])
	require.NoError(t, err)
	require.False(t, api.IsConfigEmpty(apiConfig))

	// create a new client with the given kubeconfig
	kubeconfig, err := BuildKubernetesRESTConfig(*apiConfig)
	require.NoError(t, err)

	s := runtime.NewScheme()
	builder := append(runtime.SchemeBuilder{}, corev1.AddToScheme)
	builder = append(builder, schemeAdders...)
	require.NoError(t, builder.AddToScheme(s))
	namespaceAccessClient, err := client.New(kubeconfig, client.Options{
		Scheme: s,
	})
	require.NoError(t, err)
	return namespaceAccessClient, adminSecret
}

// ValidateKubeClient validates the the kube client can access the given namespace by listing objects using the provided list instance
// The list is checked to not be empty.
func ValidateKubeClient(t *testing.T, namespaceAccessClient client.Client, namespace string, list client.ObjectList) {
	t.Helper()
	require.NoError(t, namespaceAccessClient.List(context.TODO(), list, client.InNamespace(namespace)))
	require.NotEmpty(t, list)
}

func setInsecureSkipTLSVerify(apiConfig api.Config) api.Config {
	for _, c := range apiConfig.Clusters {
		if c != nil {
			c.CertificateAuthorityData = nil
			c.InsecureSkipTLSVerify = true
		}
	}
	return apiConfig
}
