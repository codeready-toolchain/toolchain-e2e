package configuration

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/pkg/errors"

	quotav1 "github.com/openshift/api/quota/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultRetryInterval = time.Millisecond * 200
	DefaultTimeout       = time.Minute * 5
)

// NewClient returns a new client to the cluster defined by the current context in
// the KUBECONFIG
func NewClient(term terminal.Terminal, kubeconfigPath string) (client.Client, *rest.Config, *runtime.Scheme, error) {
	// look-up the kubeconfig to use
	kubeconfigFile, err := getKubeconfigFile(kubeconfigPath)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "error while locating KUBECONFIG")
	}
	term.Debugf("📔 using kubeconfig at %s", kubeconfigFile.Name())
	kubeconfig, err := newKubeConfig(kubeconfigFile)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "error while loading KUBECONFIG")
	}
	s := scheme.Scheme
	b := newSchemeBuilder()
	if err := b.AddToScheme(scheme.Scheme); err != nil {
		return nil, nil, nil, errors.Wrap(err, "cannot configure scheme")
	}
	clientConfig, err := kubeconfig.ClientConfig()
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "cannot create client config")
	}

	cl, err := client.New(clientConfig, client.Options{Scheme: s})
	return cl, clientConfig, s, err
}

// AddToScheme adds all Resources to the Scheme
func newSchemeBuilder() runtime.SchemeBuilder {
	return append(apis.AddToSchemes, quotav1.Install)
}

// GetKubeconfigFile returns a file reader on (by order of match):
// - the --kubeconfig CLI argument if it was provided
// - the $KUBECONFIG file it the env var was set
// - the <user_home_dir>/.kube/config file
func getKubeconfigFile(kubeconfigPath string) (*os.File, error) {
	var path string
	if kubeconfigPath != "" {
		path = kubeconfigPath
	} else if kubeconfigPath = os.Getenv("KUBECONFIG"); kubeconfigPath != "" {
		path = kubeconfigPath
	} else {
		path = filepath.Join(homeDir(), ".kube", "config")
	}
	return os.Open(path)
}

func newKubeConfig(r io.Reader) (clientcmd.ClientConfig, error) {
	d, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return clientcmd.NewClientConfigFromBytes(d)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
