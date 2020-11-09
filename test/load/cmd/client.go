package cmd

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"

	"github.com/mitchellh/go-homedir"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func newClient() (*restclient.RESTClient, *restclient.Config, error) {
	kubeconfig, err := contextConfig()
	if err != nil {
		return nil, nil, err
	}
	scheme := runtime.NewScheme()
	toolchainv1alpha1.SchemeBuilder.Register(
		&toolchainv1alpha1.UserSignup{},
		&toolchainv1alpha1.UserSignupList{},
		&toolchainv1alpha1.BannedUser{},
		&toolchainv1alpha1.BannedUserList{},
	)
	if err = toolchainv1alpha1.SchemeBuilder.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	config := kubeconfig
	config.GroupVersion = &toolchainv1alpha1.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	client, err := rest.RESTClientFor(config)
	return client, config, err
}

// contextConfig returns the config of the current context in kubeconfig
func contextConfig() (*restclient.Config, error) {
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

var httpClient = &http.Client{
	Timeout: time.Second * 10,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

func defaultKubeConfig() string {
	if k := os.Getenv("KUBECONFIG"); k != "" {
		return k
	}
	if home, _ := homedir.Dir(); home != "" { // ignore the error, so there is no default value available
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}
