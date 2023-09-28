package util

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

func BuildKubernetesClient(apiConfig api.Config) (*rest.Config, error) {
	insecureAPIConfig := setInsecureSkipTLSVerify(apiConfig)

	configOverrides := clientcmd.ConfigOverrides{}
	return clientcmd.NewDefaultClientConfig(insecureAPIConfig, &configOverrides).ClientConfig()
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
