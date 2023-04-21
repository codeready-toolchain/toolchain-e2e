package parallel

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	v1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestWebConsoleDeployedSuccessfully(t *testing.T) {
	// given
	await := WaitForDeployments(t)

	for i, memberAwait := range await.AllMembers() {

		image := memberAwait.GetContainerEnv(t, "MEMBER_OPERATOR_WEBCONSOLEPLUGIN_IMAGE")
		require.NotEmpty(t, image, "The value of the env var MEMBER_OPERATOR_WEBCONSOLEPLUGIN_IMAGE wasn't found in the deployment of the member operator.")

		_, err := memberAwait.WaitForServiceAccount(t, memberAwait.Namespace, "member-operator-console-plugin")
		require.NoError(t, err)
		waitForWebConsolePluginRole(t, memberAwait)
		waitForWebConsolePluginRoleBinding(t, memberAwait)
		waitForWebConsolePluginDeployment(t, memberAwait, image)
		waitForWebConsolePluginService(t, memberAwait)

		signupRequest := NewSignupRequest(await).
			Username(fmt.Sprintf("consoletest%d", i)).
			Email("consoletest@redhat.com").
			TargetCluster(memberAwait).
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t)
		response := signupRequest.GetSignupResponse(t)

		// Response should contain a ConsoleURL with a value something like:
		// https://console-openshift-console.apps.99b682869228f7464338-mgmt.ci.hypershift.devcluster.openshift.com/
		require.Contains(t, response, "consoleURL")

		// Since we can't easily access the web console API resources directly (due to complex security requirements) we
		// will instead create a route in the member cluster with which to access the console plugin

		// Before we can create the route however, we need the certificate and private key values from the
		// member-operator-console-plugin secret
		secret, err := memberAwait.WaitForSecret(t, "member-operator-console-plugin")
		require.NoError(t, err)

		// Now we can create the route resource
		route := &v1.Route{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "consolepluginroute",
				Namespace: memberAwait.Namespace,
				Annotations: map[string]string{
					"openshift.io/host.generated": "true",
				},
			},
			Spec: v1.RouteSpec{
				To: v1.RouteTargetReference{
					Kind: "Service",
					Name: "member-operator-console-plugin",
				},
				Port: &v1.RoutePort{
					TargetPort: intstr.FromInt(9443),
				},
				TLS: &v1.TLSConfig{
					Termination: "reencrypt",
					Certificate: string(secret.Data["tls.crt"]),
					Key:         string(secret.Data["tls.key"]),
				},
				WildcardPolicy: "None",
			},
		}

		err = memberAwait.CreateWithCleanup(t, route)
		require.NoError(t, err)

		reloaded, err := memberAwait.WaitForRouteToBeAvailable(t, route.Namespace, route.Name, "/status")
		require.NoError(t, err, "route not available", route)

		// Construct the routeURL from the route host
		routeURL := reloaded.Spec.Host + "/"

		manifestURL := fmt.Sprintf("%s%s%s", "https://", routeURL, "plugin-manifest.json")
		healthCheckURL := fmt.Sprintf("%s%s%s", "https://", routeURL, "status")

		var tr *http.Transport
		{ // #nosec G402
			tr = &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}
		}

		httpClient := &http.Client{Transport: tr}

		var healthCheckResponse *http.Response

		// at this point, since the test is not executed as the first one in the whole e2e test suite,
		// we expect that the service should be already healthy, thus we don't need to pool because waiting
		// another minute shouldn't have any impact on the outcome. At the same time the presence
		// of all Web console plugins related resources are verified at the beginning of thi test including
		// the availability of the deployment. In other words, if it fails, then there is definitely
		// some problem with the service.
		req, err := http.NewRequest("GET", healthCheckURL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", signupRequest.GetToken())

		healthCheckResponse, err = httpClient.Do(req) //nolint
		require.NoError(t, err)
		defer healthCheckResponse.Body.Close()
		require.Equal(t, http.StatusOK, healthCheckResponse.StatusCode, "error calling health check endpoint", route)

		req, err = http.NewRequest("GET", manifestURL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", signupRequest.GetToken())

		manifestResponse, err := httpClient.Do(req)
		require.NoError(t, err)
		defer manifestResponse.Body.Close()
		require.Equal(t, http.StatusOK, healthCheckResponse.StatusCode, "error calling console plugin manifests", route)

		body, err := io.ReadAll(manifestResponse.Body)
		require.NoError(t, err)

		require.True(t, strings.HasPrefix(string(body), "{\n  \"name\": \"toolchain-member-web-console-plugin\","))
	}
}

func waitForWebConsolePluginRole(t *testing.T, await *wait.MemberAwaitility) {
	ns := &corev1.Namespace{}
	ns.Name = await.Namespace
	role, err := await.WaitForRole(t, ns, "member-operator-console-plugin")
	require.NoError(t, err)
	assert.Len(t, role.Rules, 2)
	expected := &rbacv1.Role{
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"toolchain.dev.openshift.com"},
				Resources: []string{"memberoperatorconfigs"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	assert.Equal(t, expected.Rules, role.Rules)
	assert.Equal(t, "codeready-toolchain", role.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
}

func waitForWebConsolePluginRoleBinding(t *testing.T, await *wait.MemberAwaitility) {
	ns := &corev1.Namespace{}
	ns.Name = await.Namespace
	rb, err := await.WaitForRoleBinding(t, ns, "member-operator-console-plugin")
	require.NoError(t, err)
	assert.Len(t, rb.Subjects, 1)
	assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
	assert.Equal(t, "member-operator-console-plugin", rb.Subjects[0].Name)
	assert.Equal(t, "member-operator-console-plugin", rb.RoleRef.Name)
	assert.Equal(t, "Role", rb.RoleRef.Kind)
	assert.Equal(t, "rbac.authorization.k8s.io", rb.RoleRef.APIGroup)
	assert.Equal(t, "codeready-toolchain", rb.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
}

func waitForWebConsolePluginDeployment(t *testing.T, await *wait.MemberAwaitility, image string) {
	t.Logf("checking Deployment '%s' in namespace '%s' and with image '%s'", "member-operator-console-plugin", await.Namespace, image)
	actualDeployment := await.WaitForDeploymentToGetReady(t, "member-operator-console-plugin", 3,
		wait.DeploymentHasContainerWithImage("member-operator-console-plugin", image))

	assert.Equal(t, "codeready-toolchain", actualDeployment.ObjectMeta.Labels["toolchain.dev.openshift.com/provider"])
	assert.Equal(t, int32(3), *actualDeployment.Spec.Replicas)
	assert.Equal(t, map[string]string{
		"name": "member-operator-console-plugin",
	}, actualDeployment.Spec.Selector.MatchLabels)

	template := actualDeployment.Spec.Template
	assert.Equal(t, map[string]string{
		"name": "member-operator-console-plugin",
		"run":  "member-operator-console-plugin",
	}, template.ObjectMeta.Labels)
	assert.Equal(t, "member-operator-console-plugin", template.Spec.ServiceAccountName)
	require.Len(t, template.Spec.Volumes, 1)
	assert.Equal(t, "consoleplugin-certs", template.Spec.Volumes[0].Name)
	assert.Equal(t, "member-operator-console-plugin", template.Spec.Volumes[0].Secret.SecretName)
	require.Len(t, template.Spec.Containers, 1)

	container := template.Spec.Containers[0]
	assert.Equal(t, "member-operator-console-plugin", container.Name)
	assert.NotEmpty(t, container.Image)
	assert.Equal(t, []string{"member-operator-console-plugin"}, container.Command)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.NotEmpty(t, container.LivenessProbe)
	assert.NotEmpty(t, container.ReadinessProbe)
	assert.NotEmpty(t, container.StartupProbe)
	assert.Len(t, container.Env, 1)
	assert.Equal(t, "WATCH_NAMESPACE", container.Env[0].Name)
	assert.Equal(t, await.Namespace, container.Env[0].Value)

	assert.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "consoleplugin-certs", container.VolumeMounts[0].Name)
	assert.Equal(t, "/etc/consoleplugin/certs", container.VolumeMounts[0].MountPath)
	assert.True(t, container.VolumeMounts[0].ReadOnly)
}

func waitForWebConsolePluginService(t *testing.T, await *wait.MemberAwaitility) {
	t.Logf("waiting for Service '%s' in namespace '%s'", "member-operator-console-plugin", await.Namespace)
	service, err := await.WaitForService(t, "member-operator-console-plugin")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"run":                                  "member-operator-console-plugin",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}, service.Labels)
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(9443), service.Spec.Ports[0].Port)
	assert.Equal(t, "9443", service.Spec.Ports[0].Name)
	assert.Equal(t, intstr.IntOrString{
		IntVal: 9443,
	}, service.Spec.Ports[0].TargetPort)
	assert.Equal(t, map[string]string{
		"run": "member-operator-console-plugin",
	}, service.Spec.Selector)
}
