package wait

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultOperatorRetryInterval     = time.Millisecond * 500
	DefaultOperatorTimeout           = time.Second * 120
	DefaultRetryInterval             = time.Millisecond * 100 // make it short because a "retry interval" is waited before the first test
	DefaultTimeout                   = time.Second * 60
	MemberNsVar                      = "MEMBER_NS"
	HostNsVar                        = "HOST_NS"
	RegistrationServiceVar           = "REGISTRATION_SERVICE_NS"
	ToolchainClusterConditionTimeout = 180 * time.Second
)

type RetryIntervalOption time.Duration
type TimeoutOption time.Duration

// Awaitility contains information necessary for verifying availability of resources in both operators
type Awaitility struct {
	T                      *testing.T
	Client                 framework.FrameworkClient
	ControllerClient       client.Client
	KubeClient             kubernetes.Interface
	Scheme                 *runtime.Scheme
	MemberNs               string
	HostNs                 string
	RegistrationServiceNs  string
	RegistrationServiceURL string
}

type SingleAwaitility struct {
	T               *testing.T
	Client          framework.FrameworkClient
	Ns              string
	OtherOperatorNs string
	RetryInterval   time.Duration
	Timeout         time.Duration
}

func NewSingleAwaitility(t *testing.T, client framework.FrameworkClient, operatorNS, otherOperatorNS string) *SingleAwaitility {
	return &SingleAwaitility{
		T:               t,
		Client:          client,
		Ns:              operatorNS,
		OtherOperatorNs: otherOperatorNS,
		RetryInterval:   DefaultRetryInterval,
		Timeout:         DefaultTimeout,
	}
}

// Member creates SingleAwaitility for the member operator
func (a *Awaitility) Member() *MemberAwaitility {
	return NewMemberAwaitility(a)
}

// Host creates SingleAwaitility for the host operator
func (a *Awaitility) Host() *HostAwaitility {
	return NewHostAwaitility(a)
}

// ReadyToolchainCluster is a ClusterCondition that represents cluster that is ready
var ReadyToolchainCluster = &v1alpha1.ToolchainClusterCondition{
	Type:   v1alpha1.ToolchainClusterReady,
	Status: v1.ConditionTrue,
}

// WaitForReadyToolchainClusters waits until both ToolchainClusters (host and member) exist and has ready ClusterCondition
func (a *Awaitility) WaitForReadyToolchainClusters() error {
	if err := a.Host().WaitForToolchainCluster(cluster.Member, ReadyToolchainCluster); err != nil {
		return err
	}
	//if err := a.Member().WaitForToolchainCluster(cluster.Host, ReadyToolchainCluster); err != nil {
	//	return err
	//}
	return nil
}

func (a *SingleAwaitility) WithRetryOptions(options ...interface{}) *SingleAwaitility {
	newAwait := NewSingleAwaitility(a.T, a.Client, a.Ns, a.OtherOperatorNs)
	for _, option := range options {
		switch v := option.(type) {
		case RetryIntervalOption:
			newAwait.RetryInterval = time.Duration(v)
		case TimeoutOption:
			newAwait.Timeout = time.Duration(v)
		default:
			assert.Fail(a.T, "unsupported retry option type", "actual type: %T", v)
		}
	}
	return newAwait
}

// WaitForToolchainCluster waits until there is a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. If the given condition is not nil, then it also checks
// if the CR has the ClusterCondition
func (a *SingleAwaitility) WaitForToolchainCluster(clusterType cluster.Type, condition *v1alpha1.ToolchainClusterCondition) error {
	timeout := a.Timeout
	if condition != nil {
		timeout = ToolchainClusterConditionTimeout
	}
	return wait.Poll(a.RetryInterval, timeout, func() (done bool, err error) {
		_, ok, err := a.GetToolchainCluster(clusterType, condition)
		if ok {
			return true, nil
		}
		a.T.Logf("waiting for availability of %s ToolchainCluster CR (in namespace %s) representing operator running in namespace '%s'", clusterType, a.Ns, a.OtherOperatorNs)
		return false, err
	})
}

// WaitForToolchainClusterConditionWithName waits until there is a ToolchainCluster with the given name
// and with the given ClusterCondition (if it the condition is nil, then it skips this check)
func (a *SingleAwaitility) WaitForToolchainClusterConditionWithName(name string, condition *v1alpha1.ToolchainClusterCondition) error {
	timeout := a.Timeout
	if condition != nil {
		timeout = ToolchainClusterConditionTimeout
	}
	return wait.Poll(a.RetryInterval, timeout, func() (done bool, err error) {
		cluster := &v1alpha1.ToolchainCluster{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Ns, Name: name}, cluster); err != nil {
			return false, err
		}
		if containsClusterCondition(cluster.Status.Conditions, condition) {
			a.T.Logf("found %s ToolchainCluster", name)
			return true, nil
		}
		a.T.Logf("waiting for %s ToolchainCluster having the expected condition (expected: %+v vs actual: %+v)", name, condition, cluster.Status.Conditions)
		return false, err
	})
}

// GetToolchainCluster retrieves and returns a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. If the given condition is not nil, then it also checks
// if the CR has the ClusterCondition
func (a *SingleAwaitility) GetToolchainCluster(clusterType cluster.Type, condition *v1alpha1.ToolchainClusterCondition) (v1alpha1.ToolchainCluster, bool, error) {
	clusters := &v1alpha1.ToolchainClusterList{}
	if err := a.Client.List(context.TODO(), clusters, &client.ListOptions{Namespace: a.Ns}); err != nil {
		return v1alpha1.ToolchainCluster{}, false, err
	}
	for _, cl := range clusters.Items {
		if cl.Labels["namespace"] == a.OtherOperatorNs && cluster.Type(cl.Labels["type"]) == clusterType {
			if containsClusterCondition(cl.Status.Conditions, condition) {
				a.T.Logf("found %s ToolchainCluster running in namespace '%s'", clusterType, a.OtherOperatorNs)
				return cl, true, nil
			} else {
				a.T.Logf("found %s ToolchainCluster running in namespace '%s' but with insufficient conditions: %+v", clusterType, a.OtherOperatorNs, cl.Status.Conditions)
			}
		} else {
			fmt.Printf("cluster no match %s != %s, %s != %s\n", cl.Labels["namespace"], a.OtherOperatorNs, cluster.Type(cl.Labels["type"]), clusterType)
		}
	}
	return v1alpha1.ToolchainCluster{}, false, nil
}

// ClusterOption an option to configure the cluster to use in the tests
type ClusterOption func(*v1alpha1.ToolchainCluster)

// CapacityExhausted an option to state that the cluster capacity has exhausted
var CapacityExhausted ClusterOption = func(c *v1alpha1.ToolchainCluster) {
	c.Labels["toolchain.dev.openshift.com/capacity-exhausted"] = strconv.FormatBool(true)
}

// Type sets the label which defines the type of cluster
func Type(t cluster.Type) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["type"] = string(t)
	}
}

// Owner sets the 'ownerClusterName' label
func Owner(name string) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["ownerClusterName"] = name
	}
}

// Namespace sets the 'namespace' label
func Namespace(name string) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["namespace"] = name
	}
}

// SecretRef sets the SecretRef in the cluster's Spec
func SecretRef(ref string) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.SecretRef = v1alpha1.LocalSecretReference{
			Name: ref,
		}
	}
}

// APIEndpoint sets the APIEndpoint in the cluster's Spec
func APIEndpoint(url string) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.APIEndpoint = url
	}
}

// CABundle sets the CABundle in the cluster's Spec
func CABundle(bundle string) ClusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.CABundle = bundle
	}
}

func (a *SingleAwaitility) NewToolchainCluster(name string, options ...ClusterOption) *v1alpha1.ToolchainCluster {
	toolchainCluster := &v1alpha1.ToolchainCluster{
		Spec: v1alpha1.ToolchainClusterSpec{
			SecretRef: v1alpha1.LocalSecretReference{
				Name: "", // default
			},
			APIEndpoint: "", // default
			CABundle:    "", // default
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: a.Ns,
			Labels: map[string]string{
				"type":             "member",
				"ownerClusterName": "east",
			},
		},
	}
	for _, configure := range options {
		configure(toolchainCluster)
	}
	return toolchainCluster
}

func containsClusterCondition(conditions []v1alpha1.ToolchainClusterCondition, contains *v1alpha1.ToolchainClusterCondition) bool {
	if contains == nil {
		return true
	}
	for _, c := range conditions {
		if c.Type == contains.Type {
			return contains.Status == c.Status
		}
	}
	return false
}
