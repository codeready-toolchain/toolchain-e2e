package kubeconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Modify returns a function to modify a kubeconfig stored in a secret using the supplied modifiers.
func Modify(t *testing.T, modifiers ...func(*clientcmdapi.Config)) func(*corev1.Secret) {
	return func(secret *corev1.Secret) {
		t.Helper()
		kc, err := clientcmd.Load(secret.Data["kubeconfig"])
		require.NoError(t, err)

		for _, m := range modifiers {
			m(kc)
		}

		data, err := clientcmd.Write(*kc)
		require.NoError(t, err)

		secret.Data["kubeconfig"] = data
	}
}

// ApiEndpoint is a modifier to update the "Server" of the current context in the kubeconfig.
// Intended to be used with the ModifyKubeConfig function.
func ApiEndpoint(apiEndpoint string) func(*clientcmdapi.Config) {
	return func(kc *clientcmdapi.Config) {
		c := getCurrentCluster(kc)
		c.Server = apiEndpoint
	}
}

// Token is a modifier to update the the auth token of the current context in the kubeconfig.
// Intended to be used with the ModifyKubeConfig function.
func Token(token string) func(*clientcmdapi.Config) {
	return func(kc *clientcmdapi.Config) {
		c := getCurrentAuth(kc)
		c.Token = token
	}
}

func getCurrentCluster(kc *clientcmdapi.Config) *clientcmdapi.Cluster {
	return kc.Clusters[kc.Contexts[kc.CurrentContext].Cluster]
}

func getCurrentAuth(kc *clientcmdapi.Config) *clientcmdapi.AuthInfo {
	return kc.AuthInfos[kc.Contexts[kc.CurrentContext].AuthInfo]
}
