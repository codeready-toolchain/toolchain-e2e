package wait

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultOperatorRetryInterval     = time.Millisecond * 500
	DefaultOperatorTimeout           = time.Second * 180
	DefaultRetryInterval             = time.Millisecond * 100 // make it short because a "retry interval" is waited before the first test
	DefaultTimeout                   = time.Second * 60
	MemberNsVar                      = "MEMBER_NS"
	HostNsVar                        = "HOST_NS"
	RegistrationServiceVar           = "REGISTRATION_SERVICE_NS"
	ToolchainClusterConditionTimeout = 180 * time.Second
)

type Awaitility struct {
	T             *testing.T
	Client        client.Client
	Namespace     string
	Type          cluster.Type
	RetryInterval time.Duration
	Timeout       time.Duration
	MetricsURL    string
}

// ReadyToolchainCluster is a ClusterCondition that represents cluster that is ready
var ReadyToolchainCluster = &v1alpha1.ToolchainClusterCondition{
	Type:   v1alpha1.ToolchainClusterReady,
	Status: v1.ConditionTrue,
}

// WithRetryOptions returns a new Awaitility with the given "RetryOption"s applied
func (a *Awaitility) WithRetryOptions(options ...RetryOption) *Awaitility {
	result := &Awaitility{
		T:             a.T,
		Type:          a.Type,
		Client:        a.Client,
		Namespace:     a.Namespace,
		RetryInterval: DefaultRetryInterval,
		Timeout:       DefaultTimeout,
	}
	for _, option := range options {
		option.apply(result)
	}
	return result
}

// RetryOption is some configuration that modifies options for an Awaitility.
type RetryOption interface {
	apply(*Awaitility)
}

// RetryInterval an option to configure the RetryInterval
type RetryInterval time.Duration

var _ RetryOption = RetryInterval(0)

func (o RetryInterval) apply(a *Awaitility) {
	a.RetryInterval = time.Duration(o)
}

// TimeoutOption an option to configure the Timeout
type TimeoutOption time.Duration

var _ RetryOption = TimeoutOption(0)

func (o TimeoutOption) apply(a *Awaitility) {
	a.Timeout = time.Duration(o)
}

// WaitForMetricsService waits until there's a service with the given name in the current namespace
func (a *Awaitility) WaitForMetricsService(name string) (corev1.Service, error) {
	var metricsSvc *corev1.Service
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		metricsSvc = &corev1.Service{}
		// retrieve the metrics service from the namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			metricsSvc)
		if err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for availability of service '%s' in namespace '%s'...", name, a.Namespace)
				return false, nil
			}
			return false, err
		}
		a.T.Logf("found service '%s'", metricsSvc.Name)
		return true, nil
	})
	return *metricsSvc, err
}

// WaitForToolchainClusterWithCondition waits until there is a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. If the given condition is not nil, then it also checks
// if the CR has the ClusterCondition
func (a *Awaitility) WaitForToolchainClusterWithCondition(clusterType cluster.Type, namespace string, condition *v1alpha1.ToolchainClusterCondition) (v1alpha1.ToolchainCluster, error) {
	timeout := a.Timeout
	if condition != nil {
		timeout = ToolchainClusterConditionTimeout
	}
	var c v1alpha1.ToolchainCluster
	err := wait.Poll(a.RetryInterval, timeout, func() (done bool, err error) {
		var ready bool
		if c, ready, err = a.GetToolchainCluster(clusterType, namespace, condition); ready {
			return true, nil
		}
		a.T.Logf("waiting for availability of %s ToolchainCluster CR (in namespace %s) representing operator running in namespace '%s'", clusterType, a.Namespace, a.Namespace)
		return false, err
	})
	return c, err
}

// WaitForNamedToolchainClusterWithCondition waits until there is a ToolchainCluster with the given name
// and with the given ClusterCondition (if it the condition is nil, then it skips this check)
func (a *Awaitility) WaitForNamedToolchainClusterWithCondition(name string, condition *v1alpha1.ToolchainClusterCondition) (v1alpha1.ToolchainCluster, error) {
	timeout := a.Timeout
	if condition != nil {
		timeout = ToolchainClusterConditionTimeout
	}
	c := v1alpha1.ToolchainCluster{}
	err := wait.Poll(a.RetryInterval, timeout, func() (done bool, err error) {
		c = v1alpha1.ToolchainCluster{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, &c); err != nil {
			return false, err
		}
		if containsClusterCondition(c.Status.Conditions, condition) {
			a.T.Logf("found %s ToolchainCluster", name)
			return true, nil
		}
		a.T.Logf("waiting for %s ToolchainCluster having the expected condition (expected: %+v vs actual: %+v)", name, condition, c.Status.Conditions)
		return false, err
	})
	return c, err
}

// GetToolchainCluster retrieves and returns a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. If the given condition is not nil, then it also checks
// if the CR has the ClusterCondition
func (a *Awaitility) GetToolchainCluster(clusterType cluster.Type, namespace string, condition *v1alpha1.ToolchainClusterCondition) (v1alpha1.ToolchainCluster, bool, error) {
	clusters := &v1alpha1.ToolchainClusterList{}
	if err := a.Client.List(context.TODO(), clusters, client.InNamespace(a.Namespace), client.MatchingLabels{
		"namespace": namespace,
		"type":      string(clusterType),
	}); err != nil {
		return v1alpha1.ToolchainCluster{}, false, err
	}
	if len(clusters.Items) == 0 {
		a.T.Logf("no toolchaincluster resource with expected labels: namespace='%s', type='%s'", namespace, string(clusterType))
	}
	// assume there is zero or 1 match only
	for _, cl := range clusters.Items {
		if containsClusterCondition(cl.Status.Conditions, condition) {
			a.T.Logf("found '%s' ToolchainCluster running in namespace '%s'", clusterType, namespace)
			return cl, true, nil
		}
		a.T.Logf("found %s ToolchainCluster running in namespace '%s' but with insufficient conditions: %+v", clusterType, namespace, cl.Status.Conditions)

	}
	return v1alpha1.ToolchainCluster{}, false, nil
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

// SetupRouteForService if needed, creates a route for the given service (with the same namespace/name)
// It waits until the route is avaiable (or returns an error) by first checking the resource status
// and then making a call to the given endpoint
func (a *Awaitility) SetupRouteForService(serviceName, endpoint string) (routev1.Route, error) {
	a.T.Logf("Setting up route for service '%s' with endpoint '%s'", serviceName, endpoint)
	service, err := a.WaitForMetricsService(serviceName)
	if err != nil {
		return routev1.Route{}, err
	}

	// now, create the route for the service (if needed)
	route := routev1.Route{}
	if err := a.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Name,
	}, &route); err != nil {
		require.True(a.T, errors.IsNotFound(err), "failed to get route to access the '%s' service", service.Name)
		route = routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      service.Name,
			},
			Spec: routev1.RouteSpec{
				Port: &routev1.RoutePort{
					TargetPort: intstr.FromString("http-metrics"),
				},
				TLS: &routev1.TLSConfig{
					Termination: routev1.TLSTerminationEdge,
				},
				To: routev1.RouteTargetReference{
					Kind: service.Kind,
					Name: service.Name,
				},
			},
		}
		if err = a.Client.Create(context.TODO(), &route); err != nil {
			return route, err
		}
	}
	return a.WaitForRouteToBeAvailable(route.Namespace, route.Name, endpoint)
}

// WaitForRouteToBeAvailable wais until the given route is available, ie, it has an Ingress with a host configured
// and the endpoint is reachable (with a `200 OK` status response)
func (a *Awaitility) WaitForRouteToBeAvailable(ns, name, endpoint string) (routev1.Route, error) {
	route := routev1.Route{}
	// retrieve the route for the registration service
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}, &route); err != nil {
			if errors.IsNotFound(err) {
				a.T.Logf("Waiting for creation of route '%s' in namespace '%s'...\n", name, ns)
				return false, nil
			}
			return false, err
		}
		// assume there's a single Ingress and that its host will not be empty when the route is ready
		if len(route.Status.Ingress) == 0 || route.Status.Ingress[0].Host == "" {
			a.T.Logf("Waiting for availability of route '%s' in namespace '%s'...\n", name, ns)
			return false, nil
		}
		// verify that the endpoint gives a `200 OK` response on a GET request
		var location string
		client := http.Client{
			Timeout: time.Duration(1 * time.Second),
		}
		if route.Spec.TLS != nil {
			location = "https://" + route.Status.Ingress[0].Host + endpoint
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		} else {
			location = "http://" + route.Status.Ingress[0].Host + endpoint
		}
		resp, err := client.Get(location)
		if err, ok := err.(*url.Error); ok && err.Timeout() {
			// keep waiting if there was a timeout: the endpoint is not available yet (pod is still re-starting)
			a.T.Logf("Waiting for availability of route '%s' in namespace '%s' (endpoint timeout)...\n", name, ns)
			return false, nil
		} else if err != nil {
			return false, err
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		return resp.StatusCode == http.StatusOK, nil
	})
	return route, err
}

// GetMetricValue gets the value of the metric with the given family and label key-value pair
func (a *Awaitility) GetMetricValue(family string, labels ...string) float64 {
	value, err := getMetricValue(a.MetricsURL, family, labels)
	require.NoError(a.T, err)
	return value
}

// AssertMetricReachesValue asserts that the exposed metric with the given family
// and label key-value pair reaches the expected value
func (a *Awaitility) AssertMetricReachesValue(family string, expectedValue float64, labels ...string) {
	a.T.Logf("Waiting for metric '%s{%v}' to reach '%v'", family, labels, expectedValue)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err := getMetricValue(a.MetricsURL, family, labels)
		if err != nil {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' but error occurred: %s", family, labels, expectedValue, err.Error())
			return false, nil
		}
		if value != expectedValue {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' (currently: %v)", family, labels, expectedValue, value)
			return false, nil
		}
		return true, nil
	})
	require.NoError(a.T, err)
}

// WaitUntilMetricHasValueOrMore waits until the exposed metric with the given family
// and label key-value pair has reached the expected value (or more)
func (a *Awaitility) WaitUntilMetricHasValueOrMore(family string, expectedValue float64, labels ...string) error {
	a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or more", family, labels, expectedValue)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err := getMetricValue(a.MetricsURL, family, labels)
		if err != nil {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or more but error occurred: %s", family, labels, expectedValue, err.Error())
			return false, nil
		}
		if value < expectedValue {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or more (currently: %v)", family, labels, expectedValue, value)
			return false, nil
		}
		return true, nil
	})
	return err
}

// WaitUntilMetricHasValueOrLess waits until the exposed metric with the given family
// and label key-value pair has reached the expected value (or less)
func (a *Awaitility) WaitUntilMetricHasValueOrLess(family string, expectedValue float64, labels ...string) error {
	a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or less", family, labels, expectedValue)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err := getMetricValue(a.MetricsURL, family, labels)
		if err != nil {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or less but error occurred: %s", family, labels, expectedValue, err.Error())
			return false, nil
		}
		if value > expectedValue {
			a.T.Logf("Waiting for metric '%s{%v}' to reach '%v' or less (currently: %v)", family, labels, expectedValue, value)
			return false, nil
		}
		return true, nil
	})
	return err
}

// DeletePods deletes the pods matching the given criteria
func (a *Awaitility) DeletePods(criteria ...client.ListOption) error {
	pods := corev1.PodList{}
	err := a.Client.List(context.TODO(), &pods, criteria...)
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		if err := a.Client.Delete(context.TODO(), &p); err != nil {
			return err
		}
	}
	return nil
}

// GetMemoryUsage retrieves the memory usage (in KB) of a given the pod
func (a *Awaitility) GetMemoryUsage(podname, ns string) (int64, error) {
	podMetrics := metrics.PodMetrics{}
	if err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: ns,
			Name:      podname,
		}, &podMetrics); err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		if len(podMetrics.Containers) != 1 {
			return false, nil // keep waiting
		}
		return true, nil
	}); err != nil {
		return -1, err
	}
	// assume the pod is running a single container
	return podMetrics.Containers[0].Usage.Memory().ScaledValue(resource.Kilo), nil
}
