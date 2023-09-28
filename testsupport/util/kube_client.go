package util

import (
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const EnvDisableKubeClientTLSVerify string = "DISABLE_KUBE_CLIENT_TLS_VERIFY"

func BuildKubernetesClient(apiConfig api.Config) (*rest.Config, error) {
	if os.Getenv(EnvDisableKubeClientTLSVerify) == "true" {
		apiConfig = setInsecureSkipTLSVerify(apiConfig)
	}

	configOverrides := clientcmd.ConfigOverrides{}
	return clientcmd.NewDefaultClientConfig(apiConfig, &configOverrides).ClientConfig()
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
