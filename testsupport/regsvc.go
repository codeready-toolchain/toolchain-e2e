package testsupport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// InvokeEndpoint invokes given http URL and returns the json body response
func InvokeEndpoint(t *testing.T, method, path, authToken, requestBody string, requiredStatus int) map[string]interface{} {
	var reqBody io.Reader
	t.Logf("invoking http request: %s %s", method, path)
	if requestBody != "" {
		t.Logf("request body: %s", requestBody)
		reqBody = strings.NewReader(requestBody)
	}
	req, err := http.NewRequest(method, path, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer.Close(...)`
	if resp != nil {
		t.Logf("response status code: %d", resp.StatusCode)
	}
	require.NoError(t, err)
	defer Close(t, resp)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, requiredStatus, resp.StatusCode, "unexpected response status with body: %s", body)

	mp := make(map[string]interface{})
	if len(body) > 0 {
		err = json.Unmarshal(body, &mp)
		require.NoError(t, err)
	}
	return mp
}

// ParseResponse parses a given http response body
func ParseResponse(t *testing.T, responseBody map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	// Check that the response looks fine
	status, ok := responseBody["status"].(map[string]interface{})
	require.True(t, ok)
	return responseBody, status
}
