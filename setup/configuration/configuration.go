package configuration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"

	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	OauthNS   = "openshift-authentication"
	OauthName = "oauth-openshift"

	DefaultHostNS        = "toolchain-host-operator"
	HostOperatorWorkload = "host-operator-controller-manager"

	DefaultMemberNS        = "toolchain-member-operator"
	MemberOperatorWorkload = "member-operator-controller-manager"

	CustomTemplateUsersParam  = "custom"
	DefaultTemplateUsersParam = "default"
)

var (
	HostOperatorNamespace   string
	MemberOperatorNamespace string
	Testname                string

	DefaultRetryInterval = time.Millisecond * 200
	DefaultTimeout       = time.Minute * 5

	UserSpaceTier = "base1ns"

	resultsDir       string
	resultsFilepath  string
	stdOutFilepath   string
	stdErrFilepath   string
	startedTimestamp = time.Now().Format("2006-01-02_15:04:05")
)

func Init(term terminal.Terminal) {
	pwd, err := os.Getwd()
	if err != nil {
		term.Fatalf(err, "error getting current working directory")
	}
	resultsDir = pwd + "/tmp/results/"
	if err := os.MkdirAll(resultsDir, os.ModePerm); err != nil {
		term.Fatalf(err, "error creating results directory %s", resultsDir)
	}
	if len(Testname) > 0 && Testname[0] != '-' {
		Testname = "-" + Testname
	}
	resultsFilepath = fmt.Sprintf("%s%s%s.csv", resultsDir, startedTimestamp, Testname)
	stdOutFilepath = fmt.Sprintf("%s%s%s-stdout.log", resultsDir, startedTimestamp, Testname)
	stdErrFilepath = fmt.Sprintf("%s%s%s-stderr.log", resultsDir, startedTimestamp, Testname)
}

// NewClient returns a new client to the cluster defined by the current context in
// the KUBECONFIG
func NewClient(term terminal.Terminal, kubeconfigPath string) (client.Client, *rest.Config, *runtime.Scheme, error) {
	// look-up the kubeconfig to use
	kubeconfigFile, err := getKubeconfigFile(kubeconfigPath)
	if err != nil {
		term.Fatalf(err, "error while locating KUBECONFIG")
	}
	term.Debugf("📔 using kubeconfig at %s", kubeconfigFile.Name())
	kubeconfig, err := newKubeConfig(kubeconfigFile)
	if err != nil {
		term.Fatalf(err, "error while loading KUBECONFIG")
	}
	s, err := NewScheme()
	if err != nil {
		term.Fatalf(err, "cannot configure scheme")
	}
	clientConfig, err := kubeconfig.ClientConfig()
	if err != nil {
		term.Fatalf(err, "cannot create client config")
	}

	// Set QPS and Burst to higher values to avoid client-side throttling issues
	// prometheus uses these QPS and Burst values so it shouldn't be an issue, see https://github.com/prometheus-operator/prometheus-operator/blob/9d68ecf289d711c66bef39d2f83429265abc6986/pkg/k8sutil/k8sutil.go#L96-L97
	clientConfig.QPS = 100
	clientConfig.Burst = 100

	cl, err := client.New(clientConfig, client.Options{Scheme: s})
	term.Infof("API endpoint: %s", clientConfig.Host)
	return cl, clientConfig, s, err
}

// NewScheme returns the scheme configured with all the needed types
func NewScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	builder := append(
		runtime.SchemeBuilder{},
		toolchainv1alpha1.AddToScheme,
		quotav1.Install,
		operatorsv1alpha1.AddToScheme,
		operatorsv1.AddToScheme,
		templatev1.Install,
		routev1.Install,
		appsv1.AddToScheme,
	)
	err := builder.AddToScheme(s)
	return s, err
}

func ConfigureDefaultSpaceTier(cl client.Client) error {
	// ensure the NSTemplateTier (SpaceTier) exists
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: UserSpaceTier, Namespace: HostOperatorNamespace}, &toolchainv1alpha1.NSTemplateTier{}); err != nil {
		return err
	}

	toolchainCfg := &toolchainv1alpha1.ToolchainConfig{}
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: "config", Namespace: HostOperatorNamespace}, toolchainCfg); err != nil {
		return err
	}

	toolchainCfg.Spec.Host.Tiers.DefaultSpaceTier = &UserSpaceTier
	return cl.Update(context.TODO(), toolchainCfg)
}

// DisableCopiedCSVs disables OLM's CopiedCSVs feature, since OpenShift 4.13 the console no longer relies on CSVs to know which operators are installed
func DisableCopiedCSVs(cl client.Client) error {
	olmConfig := &operatorsv1.OLMConfig{}
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, olmConfig); err != nil {
		return err
	}
	val := true
	spec := &olmConfig.Spec
	if spec.Features == nil {
		spec.Features = &operatorsv1.Features{}
	}
	spec.Features.DisableCopiedCSVs = &val
	return cl.Update(context.TODO(), olmConfig)
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
	d, err := io.ReadAll(r)
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

func ResultsDir() string {
	return resultsDir
}

func ResultsFilepath() string {
	return resultsFilepath
}

func StdOutFilepath() string {
	return stdOutFilepath
}

func StdErrFilepath() string {
	return stdErrFilepath
}

func StartedTimestamp() string {
	return startedTimestamp
}
