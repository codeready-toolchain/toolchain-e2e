package testsupport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type HTTPRequest struct {
	body []byte
	t    *testing.T
}

func NewHTTPRequest(t *testing.T) *HTTPRequest {
	return &HTTPRequest{
		t: t,
	}
}

// InvokeEndpoint invokes given http URL and returns the json body response
func (h HTTPRequest) InvokeEndpoint(method, path, authToken, requestBody string, requiredStatus int) *HTTPRequest {
	var reqBody io.Reader
	h.t.Logf("invoking http request: %s %s", method, path)
	if requestBody != "" {
		h.t.Logf("request body: %s", requestBody)
		reqBody = strings.NewReader(requestBody)
	}
	req, err := http.NewRequest(method, path, reqBody)
	require.NoError(h.t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer.Close(...)`

	if resp != nil {
		h.t.Logf("response status code: %d", resp.StatusCode)
	}
	require.NoError(h.t, err)
	defer Close(h.t, resp)

	h.body, err = io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	require.NotNil(h.t, h.body)
	require.Equal(h.t, requiredStatus, resp.StatusCode, "unexpected response status with body: %s", h.body)
	return &h
}

// UnmarshalMap unmarshal the response body into a map type
func (h HTTPRequest) UnmarshalMap() map[string]interface{} {
	mp := make(map[string]interface{})
	if len(h.body) > 0 {
		err := json.Unmarshal(h.body, &mp)
		require.NoError(h.t, err)
	}
	return mp
}

// UnmarshalSlice unmarshal the response body into a slice of maps
func (h HTTPRequest) UnmarshalSlice() []map[string]interface{} {
	var response []map[string]interface{}
	if len(h.body) > 0 {
		err := json.Unmarshal(h.body, &response)
		require.NoError(h.t, err)
	}
	return response
}

// ParseSignupResponse parses a given http response body according to the Signup type
func ParseSignupResponse(t *testing.T, responseBody map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	// Check that the response looks fine
	status, ok := responseBody["status"].(map[string]interface{})
	require.True(t, ok)
	return responseBody, status
}
