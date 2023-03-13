package testsupport

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func VerifyProxyPlugin(t *testing.T, hostAwait *wait.HostAwaitility, proxyPluginName string) *toolchainv1alpha1.ProxyPlugin {
	proxyPlugin, err := hostAwait.WaitForProxyPlugin(t, proxyPluginName)
	require.NoError(t, err)
	return proxyPlugin
}

func CreateProxyPluginWithCleanup(t *testing.T, hostAwait *wait.HostAwaitility, proxyPluginName, routeNamespace, routeName string) *toolchainv1alpha1.ProxyPlugin {
	proxyPlugin := NewProxyPlugin(hostAwait.Namespace, proxyPluginName, routeNamespace, routeName)
	err := hostAwait.CreateWithCleanup(t, proxyPlugin)
	require.NoError(t, err)
	return proxyPlugin
}

func NewProxyPlugin(proxyPluginNamespace, proxyPluginName, routeNamespace, routeName string) *toolchainv1alpha1.ProxyPlugin {
	return &toolchainv1alpha1.ProxyPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: proxyPluginNamespace,
			Name:      proxyPluginName,
		},
		Spec: toolchainv1alpha1.ProxyPluginSpec{
			OpenShiftRouteTargetEndpoint: &toolchainv1alpha1.OpenShiftRouteTarget{
				Namespace: routeNamespace,
				Name:      routeName,
			},
		},
	}
}
