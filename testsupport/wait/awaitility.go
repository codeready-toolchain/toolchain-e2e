package wait

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	cd "github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-common/pkg/status"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-common/pkg/test/assertions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/metrics"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/util/podutils"
	k8smetrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultRetryInterval             = time.Millisecond * 100 // make it short because a "retry interval" is waited before the first test
	DefaultTimeout                   = time.Second * 120
	MemberNsVar                      = "MEMBER_NS"
	MemberNsVar2                     = "MEMBER_NS_2"
	HostNsVar                        = "HOST_NS"
	RegistrationServiceVar           = "REGISTRATION_SERVICE_NS"
	ToolchainClusterConditionTimeout = 180 * time.Second
)

type Awaitility struct {
	Client         client.Client
	RestConfig     *rest.Config
	ClusterName    string
	Namespace      string
	RetryInterval  time.Duration
	Timeout        time.Duration
	MetricsURL     string
	baselineValues map[string]float64
}

func (a *Awaitility) GetClient() client.Client {
	return a.Client
}

func (a *Awaitility) copy() *Awaitility {
	result := new(Awaitility)
	*result = *a
	return result
}

// WithRetryOptions returns a new Awaitility with the given "RetryOption"s applied
func (a *Awaitility) WithRetryOptions(options ...RetryOption) *Awaitility {
	result := a.copy()
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

// WaitForMetricDelta waits for the metric value to reach the adjusted value. The adjusted value is the delta value combined with the baseline value.
func (a *Awaitility) WaitForMetricDelta(t *testing.T, family string, delta float64, labels ...string) {
	// The delta is relative to the starting value, eg. If there are 3 usersignups when a test is started and we are waiting
	// for 2 more usersignups to be created (delta is +2) then the actual metric value (adjustedValue) we're waiting for is 5
	key := a.baselineKey(t, family, labels...)
	adjustedValue := a.baselineValues[key] + delta
	a.WaitUntiltMetricHasValue(t, family, adjustedValue, labels...)
}

// WaitForMetricBaseline waits for the metric value to reach the baseline value back (to be used during the cleanup)
func (a *Awaitility) WaitForMetricBaseline(t *testing.T, family string, labels ...string) {
	t.Log("waiting until host metrics reached their baseline again...")
	key := a.baselineKey(t, family, labels...)
	a.WaitUntiltMetricHasValue(t, family, a.baselineValues[key], labels...)
}

// generates a key to retain the baseline metric value, by joining the metric name and its labels.
// Note: there are probably more sophisticated ways to combine the name and the labels, but for now
// this simple concatenation should be enough to make the keys unique
func (a *Awaitility) baselineKey(t *testing.T, name string, labelAndValues ...string) string {
	if len(labelAndValues)%2 != 0 {
		t.Fatal("`labelAndValues` must be pairs of labels and values")
	}
	return strings.Join(append([]string{name}, labelAndValues...), ",")
}

// WaitForService waits until there's a service with the given name in the current namespace
func (a *Awaitility) WaitForService(t *testing.T, name string) (corev1.Service, error) {
	t.Logf("waiting for Service '%s' in namespace '%s'", name, a.Namespace)
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
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return *metricsSvc, err
}

// WaitForToolchainClusterWithCondition waits until there is a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. It also checks if the CR has the ClusterConditionType
func (a *Awaitility) WaitForToolchainClusterWithCondition(t *testing.T, namespace string, cdtype toolchainv1alpha1.ConditionType) (toolchainv1alpha1.ToolchainCluster, error) {
	t.Logf("waiting for ToolchainCluster in namespace '%s'", namespace)

	var c toolchainv1alpha1.ToolchainCluster
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		var ready bool
		if c, ready, err = a.GetToolchainCluster(t, namespace, cdtype); ready {
			return true, nil
		}
		return false, err
	})
	return c, err
}

// GetToolchainCluster retrieves and returns a ToolchainCluster representing a operator of the given type
// and running in the given expected namespace. It also checks if the CR has the ClusterConditionType
func (a *Awaitility) GetToolchainCluster(t *testing.T, namespace string, cdtype toolchainv1alpha1.ConditionType) (toolchainv1alpha1.ToolchainCluster, bool, error) {
	clusters := &toolchainv1alpha1.ToolchainClusterList{}
	if err := a.Client.List(context.TODO(), clusters, client.InNamespace(a.Namespace), client.MatchingLabels{
		"namespace": namespace,
	}); err != nil {
		return toolchainv1alpha1.ToolchainCluster{}, false, err
	}
	if len(clusters.Items) == 0 {
		t.Logf("no toolchaincluster resource with expected labels: namespace='%s'", namespace)
	}
	// assume there is zero or 1 match only
	for _, cl := range clusters.Items {
		if cd.IsTrue(cl.Status.Conditions, cdtype) {
			return cl, true, nil
		}
	}
	return toolchainv1alpha1.ToolchainCluster{}, false, nil
}

// SetupRouteForService if needed, creates a route for the given service (with the same namespace/name)
// It waits until the route is available (or returns an error) by first checking the resource status
// and then making a call to the given endpoint
func (a *Awaitility) SetupRouteForService(t *testing.T, serviceName, endpoint string) (routev1.Route, error) {
	t.Logf("setting up route for service '%s' with endpoint '%s'", serviceName, endpoint)
	service, err := a.WaitForService(t, serviceName)
	if err != nil {
		return routev1.Route{}, err
	}

	// now, create the route for the service (if needed)
	route := routev1.Route{}
	if err := a.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Name,
	}, &route); err != nil {
		require.True(t, apierrors.IsNotFound(err), "failed to get route to access the '%s' service: %s", service.Name, err.Error())
		route = routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      service.Name,
			},
			Spec: routev1.RouteSpec{
				Port: &routev1.RoutePort{
					TargetPort: intstr.FromString("https"),
				},
				TLS: &routev1.TLSConfig{
					Termination: routev1.TLSTerminationPassthrough,
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
	return a.WaitForRouteToBeAvailable(t, route.Namespace, route.Name, endpoint)
}

// WaitForRouteToBeAvailable waits until the given route is available, ie, it has an Ingress with a host configured
// and the endpoint is reachable (with a `200 OK` status response)
func (a *Awaitility) WaitForRouteToBeAvailable(t *testing.T, ns, name, endpoint string) (routev1.Route, error) {
	t.Logf("waiting for route '%s' in namespace '%s'", name, ns)
	route := routev1.Route{}
	// retrieve the route for the registration service
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}, &route); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		// assume there's a single Ingress and that its host will not be empty when the route is ready
		if len(route.Status.Ingress) == 0 || route.Status.Ingress[0].Host == "" {
			return false, nil
		}
		// verify that the endpoint gives a `200 OK` response on a GET request
		client := http.Client{
			Timeout: time.Duration(5 * time.Second), // because sometimes the network connection may be a bit slow
		}
		var request *http.Request

		if route.Spec.TLS != nil {
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // nolint:gosec
				},
			}
			request, err = http.NewRequest("GET", "https://"+route.Status.Ingress[0].Host+endpoint, nil)
			if err != nil {
				return false, err
			}
			request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.RestConfig.BearerToken))
		} else {
			request, err = http.NewRequest("GET", "http://"+route.Status.Ingress[0].Host+endpoint, nil)
			if err != nil {
				return false, err
			}
		}
		resp, err := client.Do(request)
		urlError := &url.Error{}
		if errors.As(err, &urlError) && urlError.Timeout() {
			// keep waiting if there was a timeout: the endpoint is not available yet (pod is still re-starting)
			return false, nil
		} else if err != nil {
			return false, err
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			return false, nil
		}
		return true, nil
	})
	return route, err
}

// GetMetricValue gets the value of the metric with the given family and label key-value pair
// fails if the metric with the given labelAndValues does not exist
func (a *Awaitility) GetMetricValue(t *testing.T, family string, labelAndValues ...string) float64 {
	value, err := metrics.GetMetricValue(a.RestConfig, a.MetricsURL, family, labelAndValues)
	require.NoError(t, err)
	return value
}

// GetMetricValue gets the value of the metric with the given family and label key-value pair
// fails if the metric with the given labelAndValues does not exist
func (a *Awaitility) GetMetricLabels(t *testing.T, family string) []map[string]string {
	labels, err := metrics.GetMetricLabels(a.RestConfig, a.MetricsURL, family)
	require.NoError(t, err)
	return labels
}

// GetMetricValue gets the value of the metric with the given family and label key-value pair
// return 0 if the metric with the given labelAndValues does not exist
func (a *Awaitility) GetMetricValueOrZero(t *testing.T, family string, labelAndValues ...string) float64 {
	if len(labelAndValues)%2 != 0 {
		t.Fatal("`labelAndValues` must be pairs of labels and values")
	}
	if value, err := metrics.GetMetricValue(a.RestConfig, a.MetricsURL, family, labelAndValues); err == nil {
		return value
	}
	return 0
}

// WaitUntiltMetricHasValue asserts that the exposed metric with the given family
// and label key-value pair reaches the expected value
func (a *Awaitility) WaitUntiltMetricHasValue(t *testing.T, family string, expectedValue float64, labels ...string) {
	t.Logf("waiting for metric '%s{%v}' to reach '%v'", family, labels, expectedValue)
	var value float64
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err = metrics.GetMetricValue(a.RestConfig, a.MetricsURL, family, labels)
		// if error occurred, ignore and return `false` to keep waiting (may be due to endpoint temporarily unavailable)
		// unless the expected value is `0`, in which case the metric is bot exposed (value==0 and err!= nil), but it's fine too.
		return (value == expectedValue && err == nil) || (expectedValue == 0 && value == 0), nil
	})
	require.NoError(t, err, "waited for metric '%s{%v}' to reach '%v'. Current value: %v", family, labels, expectedValue, value)
}

// WaitUntilMetricHasValueOrMore waits until the exposed metric with the given family
// and label key-value pair has reached the expected value (or more)
func (a *Awaitility) WaitUntilMetricHasValueOrMore(t *testing.T, family string, expectedValue float64, labels ...string) error {
	t.Logf("waiting for metric '%s{%v}' to reach '%v' or more", family, labels, expectedValue)
	var value float64
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err = metrics.GetMetricValue(a.RestConfig, a.MetricsURL, family, labels)
		// if error occurred, return `false` to keep waiting (may be due to endpoint temporarily unavailable)
		return value >= expectedValue && err == nil, nil
	})
	if err != nil {
		t.Logf("waited for metric '%s{%v}' to reach '%v' or more. Current value: %v", family, labels, expectedValue, value)
	}
	return err
}

// WaitUntilMetricHasValueOrLess waits until the exposed metric with the given family
// and label key-value pair has reached the expected value (or less)
func (a *Awaitility) WaitUntilMetricHasValueOrLess(t *testing.T, family string, expectedValue float64, labels ...string) error {
	t.Logf("waiting for metric '%s{%v}' to reach '%v' or less", family, labels, expectedValue)
	var value float64
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		value, err = metrics.GetMetricValue(a.RestConfig, a.MetricsURL, family, labels)
		// if error occurred, return `false` to keep waiting (may be due to endpoint temporarily unavailable)
		return value <= expectedValue && err == nil, nil
	})
	if err != nil {
		t.Logf("waited for metric '%s{%v}' to reach '%v' or less. Current value: %v", family, labels, expectedValue, value)
	}
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
		if err := a.Client.Delete(context.TODO(), &p); err != nil { // nolint:gosec
			return err
		}
	}
	return nil
}

// GetMemoryUsage retrieves the memory usage (in KB) of a given the pod
func (a *Awaitility) GetMemoryUsage(podname, ns string) (int64, error) {
	var containerMetrics k8smetrics.ContainerMetrics
	if err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		podMetrics := k8smetrics.PodMetrics{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: ns,
			Name:      podname,
		}, &podMetrics); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		for _, c := range podMetrics.Containers {
			if c.Name == "manager" {
				containerMetrics = c
				return true, nil
			}
		}
		return false, nil // keep waiting
	}); err != nil {
		return -1, err
	}
	// the pod contains multiple
	return containerMetrics.Usage.Memory().ScaledValue(resource.Kilo), nil
}

// CreateNamespace creates a namespace with the given name and waits until it gets active
// it also adds a deletion of the namespace at the end of the test
func (a *Awaitility) CreateNamespace(t *testing.T, name string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := a.Client.Create(context.TODO(), ns)
	require.NoError(t, err)
	err = wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		ns := &corev1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, ns); err != nil && apierrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return ns.Status.Phase == corev1.NamespaceActive, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := a.Client.Delete(context.TODO(), ns); err != nil && !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	})
}

// WaitForDeploymentToGetReady waits until the deployment with the given name is ready together with the given number of replicas
func (a *Awaitility) WaitForDeploymentToGetReady(t *testing.T, name string, replicas int, criteria ...DeploymentCriteria) *appsv1.Deployment {
	t.Logf("waiting until deployment '%s' in namespace '%s' is ready", name, a.Namespace)
	deployment := &appsv1.Deployment{}
	err := wait.Poll(a.RetryInterval, 6*a.Timeout, func() (done bool, err error) {
		deploymentConditions := status.GetDeploymentStatusConditions(context.TODO(), a.Client, name, a.Namespace)
		if err := status.ValidateComponentConditionReady(deploymentConditions...); err != nil {
			return false, nil // nolint:nilerr
		}
		deployment = &appsv1.Deployment{}
		require.NoError(t, a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, name), deployment))
		if int(deployment.Status.AvailableReplicas) != replicas {
			return false, nil
		}
		pods := &corev1.PodList{}
		require.NoError(t, a.Client.List(context.TODO(), pods, client.InNamespace(a.Namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels)))
		if len(pods.Items) != replicas {
			return false, nil
		}
		for _, pod := range pods.Items { // nolint
			if util.IsBeingDeleted(&pod) || !podutils.IsPodReady(&pod) {
				return false, nil
			}
		}
		for _, criteriaMatch := range criteria {
			if !criteriaMatch(deployment) {
				return false, nil
			}
		}
		return true, nil
	})
	require.NoError(t, err)
	return deployment
}

type DeploymentCriteria func(*appsv1.Deployment) bool

func DeploymentHasContainerWithImage(containerName, image string) DeploymentCriteria {
	return func(deployment *appsv1.Deployment) bool {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == containerName && container.Image == image {
				return true
			}
		}
		return false
	}
}

// ToolchainClusterWaitCriterion a struct to compare with an expected ToolchainCluster CR
type ToolchainClusterWaitCriterion struct {
	Match func(toolchainCluster *toolchainv1alpha1.ToolchainCluster) bool
}

// WaitForToolchainCluster waits until there is a ToolchainCluster CR available with the given list of criteria
func (a *Awaitility) WaitForToolchainCluster(t *testing.T, criteria ...ToolchainClusterWaitCriterion) (*toolchainv1alpha1.ToolchainCluster, error) {
	t.Logf("waiting for toolchaincluster in namespace '%s' to match criteria", a.Namespace)
	var clusters *toolchainv1alpha1.ToolchainClusterList
	var cl *toolchainv1alpha1.ToolchainCluster
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		clusters = &toolchainv1alpha1.ToolchainClusterList{}
		if err := a.Client.List(context.TODO(), clusters, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, obj := range clusters.Items {
			cpObj := obj
			if matchesAllCriteria := matchToolchainClusterWaitCriterion(&cpObj, criteria...); matchesAllCriteria {
				cl = &cpObj
				return true, nil
			}
		}
		return false, nil
	})
	return cl, err
}

func matchToolchainClusterWaitCriterion(actual *toolchainv1alpha1.ToolchainCluster, criteria ...ToolchainClusterWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

// UntilToolchainClusterHasName checks if ToolchainCluster has given name
func UntilToolchainClusterHasName(expectedName string) ToolchainClusterWaitCriterion {
	return ToolchainClusterWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainCluster) bool {
			return actual.Name == expectedName
		},
	}
}

// UntilToolchainClusterHasCondition checks if ToolchainCluster has the given condition
func UntilToolchainClusterHasCondition(expected toolchainv1alpha1.ConditionType) ToolchainClusterWaitCriterion {
	return ToolchainClusterWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainCluster) bool {
			return cd.IsTrue(actual.Status.Conditions, expected)
		},
	}
}

// UntilToolchainClusterHasLabels checks if ToolchainCluster has the given labels
func UntilToolchainClusterHasLabels(expected client.MatchingLabels) ToolchainClusterWaitCriterion {
	return ToolchainClusterWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainCluster) bool {
			for expectedLabelKey, expectedLabelValue := range expected {
				if actualLabelValue, found := actual.Labels[expectedLabelKey]; !found || expectedLabelValue != actualLabelValue {
					return false
				}
			}
			return true
		},
	}
}

// UntilToolchainClusterHasNoTenantLabel checks if ToolchainCluster has no tenant cluster-role label
func UntilToolchainClusterHasNoTenantLabel() ToolchainClusterWaitCriterion {
	return ToolchainClusterWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainCluster) bool {
			if _, found := actual.Labels[cluster.RoleLabel(cluster.Tenant)]; found {
				return false
			}
			return true
		},
	}
}

// UpdateToolchainCluster tries to update the Spec of the given ToolchainCluster
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated ToolchainCluster
func (a *Awaitility) UpdateToolchainCluster(t *testing.T, toolchainClusterName string, modifyToolchainCluster func(s *toolchainv1alpha1.ToolchainCluster)) (*toolchainv1alpha1.ToolchainCluster, error) {
	var tc *toolchainv1alpha1.ToolchainCluster
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		newToolchainCluster := &toolchainv1alpha1.ToolchainCluster{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: toolchainClusterName}, newToolchainCluster); err != nil {
			return true, err
		}
		modifyToolchainCluster(newToolchainCluster)
		if err := a.Client.Update(context.TODO(), newToolchainCluster); err != nil {
			t.Logf("error updating ToolchainCluster '%s': %s. Will retry again...", toolchainClusterName, err.Error())
			return false, nil
		}
		tc = newToolchainCluster
		return true, nil
	})
	return tc, err
}

// CreateWithCleanup creates the given object via client.Client.Create() and schedules the cleanup of the object at the end of the current test
func (a *Awaitility) CreateWithCleanup(t *testing.T, obj client.Object, opts ...client.CreateOption) error {
	if err := a.Client.Create(context.TODO(), obj, opts...); err != nil {
		return err
	}
	cleanup.AddCleanTasks(t, a.GetClient(), obj)
	return nil
}

// Create creates the given object via client.Client.Create()
func (a *Awaitility) Create(obj client.Object, opts ...client.CreateOption) error {
	if err := a.Client.Create(context.TODO(), obj, opts...); err != nil {
		return err
	}
	return nil
}

// Clean triggers cleanup of all resources that were marked to be cleaned before that
func (a *Awaitility) Clean(t *testing.T) {
	cleanup.ExecuteAllCleanTasks(t)
}

func (a *Awaitility) listAndPrint(t *testing.T, resourceKind, namespace string, list client.ObjectList, additionalOptions ...client.ListOption) {
	t.Logf(a.listAndReturnContent(resourceKind, namespace, list, additionalOptions...))
}

func (a *Awaitility) listAndReturnContent(resourceKind, namespace string, list client.ObjectList, additionalOptions ...client.ListOption) string {
	listOptions := additionalOptions
	if a.Namespace != "" {
		listOptions = append(additionalOptions, client.InNamespace(namespace))
	}
	if err := a.Client.List(context.TODO(), list, listOptions...); err != nil {
		return fmt.Sprintf("unable to list %s: %s", resourceKind, err)
	}
	content, _ := StringifyObjects(list)
	return fmt.Sprintf("\n%s present in the namespace:\n%s\n", resourceKind, string(content))
}

// Waiter is a helper struct for `wait.For()` that provides functions to query the cluster waiting
// for the results.
type Waiter[T client.Object] struct {
	await *Awaitility
	t     *testing.T
	gvk   schema.GroupVersionKind
}

// FirstThat uses the provided predicates to filter the objects of the type provided to `wait.For()` and
// repeatedly tries to find the first one that satisfies all the predicates.
func (w *Waiter[T]) FirstThat(predicates ...assertions.Predicate[client.Object]) (T, error) {
	w.t.Logf("waiting for objects of GVK '%s' in namespace '%s' to match criteria", w.gvk, w.await.Namespace)

	var returnedObject T
	// match status of each predicate per object
	latestResults := map[client.ObjectKey][]bool{}

	err := wait.Poll(w.await.RetryInterval, w.await.Timeout, func() (done bool, err error) {
		// because there is no generic way of figuring out the list type for some client.Object type, we need to go
		// down the low level route and use unstructured to get the list generically and unmarshal and cast the list
		// items.
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(w.gvk)
		if err := w.await.Client.List(context.TODO(), list, client.InNamespace(w.await.Namespace)); err != nil {
			return false, err
		}
		for _, obj := range list.Items {
			obj := obj // required due to memory aliasing until we upgrade to Go 1.22 which fixes this.
			object, err := w.cast(&obj)
			if err != nil {
				return false, fmt.Errorf("failed to cast the object to GVK %v: %w", w.gvk, err)
			}

			matches, results := w.matches(object, predicates)
			latestResults[client.ObjectKeyFromObject(object)] = results
			if matches {
				returnedObject = object
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		sb := strings.Builder{}
		sb.WriteString("failed to find objects (of GVK '%s') in namespace '%s' matching the criteria")
		args := []any{w.gvk, w.await.Namespace}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(w.gvk)
		if err := w.await.Client.List(context.TODO(), list, client.InNamespace(w.await.Namespace)); err != nil {
			sb.WriteString(" and also failed to retrieve the object at all with error: %s")
			args = append(args, err)
		} else {
			sb.WriteString("\nlisting the objects found in cluster with the differences from the expected state for each:")
			for _, o := range list.Items {
				o := o
				obj, _ := w.cast(&o)
				key := client.ObjectKeyFromObject(obj)

				matches := true
				objectResults := latestResults[key]
				for _, res := range objectResults {
					if !res {
						matches = false
						break
					}
				}

				sb.WriteRune('\n')
				sb.WriteString("object ")
				sb.WriteString(key.String())
				if matches {
					sb.WriteString("matches all predicates")
				} else {
					sb.WriteString("was found to have the following differences:")
					for i, res := range objectResults {
						if !res {
							sb.WriteRune('\n')
							sb.WriteString(assertions.Explain(predicates[i], obj.DeepCopyObject().(T)))
						}
					}
				}
			}
		}
		w.t.Logf(sb.String(), args...)
	}
	return returnedObject, err
}

// WithNameThat waits for a single object with the provided name in the namespace of the awaitality that additionally
// matches the provided predicates.
func (w *Waiter[T]) WithNameThat(name string, predicates ...assertions.Predicate[client.Object]) (T, error) {
	w.t.Logf("waiting for object of GVK '%s' with name '%s' in namespace '%s' to match additional criteria", w.gvk, name, w.await.Namespace)

	var returnedObject T
	latestResults := []bool{}

	err := wait.Poll(w.await.RetryInterval, w.await.Timeout, func() (done bool, err error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(w.gvk)
		if err := w.await.Client.Get(context.TODO(), client.ObjectKey{Name: name, Namespace: w.await.Namespace}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		object, err := w.cast(obj)
		if err != nil {
			return false, fmt.Errorf("failed to cast the object to GVK %v: %w", w.gvk, err)
		}

		matches, results := w.matches(object, predicates)
		latestResults = results
		if matches {
			returnedObject = object
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		sb := strings.Builder{}
		sb.WriteString("couldn't match the object (GVK '%s') called '%s' in namespace '%s' with the criteria")
		args := []any{w.gvk, name, w.await.Namespace}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(w.gvk)
		if err := w.await.Client.Get(context.TODO(), client.ObjectKey{Name: name, Namespace: w.await.Namespace}, obj); err != nil {
			sb.WriteString(" and also failed to retrieve the object at all with error: %s")
			args = append(args, err)
		} else {
			o, _ := w.cast(obj)
			sb.WriteString(" but the object exists in the cluster with the following differences:")
			for i, p := range predicates {
				if !latestResults[i] {
					expl := assertions.Explain(p, o.DeepCopyObject().(T))
					sb.WriteRune('\n')
					sb.WriteString(expl)
				}
			}
		}
		w.t.Logf(sb.String(), args...)
	}
	return returnedObject, err
}

func (w *Waiter[T]) cast(obj *unstructured.Unstructured) (T, error) {
	var empty T
	raw, err := obj.MarshalJSON()
	if err != nil {
		return empty, fmt.Errorf("failed to obtain the raw JSON of the object: %w", err)
	}

	typed, err := w.await.Client.Scheme().New(w.gvk)
	if err != nil {
		return empty, fmt.Errorf("failed to create a new empty object from the scheme: %w", err)
	}

	err = json.Unmarshal(raw, typed)
	if err != nil {
		return empty, fmt.Errorf("failed to unmarshal the raw JSON to the go structure: %w", err)
	}

	return typed.(T), nil
}

func (w *Waiter[T]) matches(obj T, predicates []assertions.Predicate[client.Object]) (bool, []bool) {
	matches := true
	results := make([]bool, len(predicates))
	for i, p := range predicates {
		res := p.Matches(obj)
		results[i] = res
		matches = matches && res
	}
	return matches, results
}

// For returns an struct using which one can wait for the objects with the same type as the one provided.
//
// Note that this is a recent addition to the utility functions and therefore is not used much. It could
// be used to cut down the line count of the new utility functions dramatically though.
func For[T client.Object](t *testing.T, a *Awaitility, obj T) *Waiter[T] {
	gvks, _, err := a.Client.Scheme().ObjectKinds(obj)
	require.NoError(t, err, "failed to get the GVK of object %v", obj)

	require.Len(t, gvks, 1, "multiple versions of a single GK not supported but found multiple for object %v", obj)

	return &Waiter[T]{
		await: a,
		t:     t,
		gvk:   gvks[0],
	}
}
