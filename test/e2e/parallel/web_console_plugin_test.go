package parallel

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	v1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestWebConsoleDeployedSuccessfully(t *testing.T) {
	// given
	await := WaitForDeployments(t)

	for i, memberAwait := range await.AllMembers() {

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
		// another minute shouldn't have any impact on the outcome. If it fails, then there is definitely
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
