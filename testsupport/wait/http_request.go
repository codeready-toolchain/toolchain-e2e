package wait

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// NewHTTPRequest creates a new http client configuration
func NewHTTPRequest() *HTTPRequest {
	cl := &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // nolint:gosec
			},
		},
	}

	return &HTTPRequest{
		client: cl,
	}
}

// HTTPRequest provides an API for creating a new HTTP request.
// Function chaining may be used to achieve an efficient "single-statement" HTTP requests creation, for example:
//
// body, status := NewHTTPRequest().
// 					Method("GET").
//					URL(route + "/api/v1/signup").
//					Token(token0).
//					RequireStatusCode(http.StatusOK).
//					ParseResponse().
//					Execute(t)
type HTTPRequest struct {
	client              *http.Client
	method              string
	url                 string
	token               string
	requireStatusCode   int
	requireResponseBody string
	body                string
	queryParams         map[string]string
	parseResponse       bool
}

// Method specifies the HTTP method to be used (GET/POST/PUT/DELETE ...)
// This is a mandatory field and should be set before invoking Execute.
func (c *HTTPRequest) Method(method string) *HTTPRequest {
	c.method = method
	return c
}

// URL specifies the URL where the request will be invoked.
// This is a mandatory field and should be set before invoking Execute.
func (c *HTTPRequest) URL(URL string) *HTTPRequest {
	c.url = URL
	return c
}

// Token specifies the auth token to be used as bear token for the HTTP request.
// This is an optional field and should be set if the endpoint is authenticated.
func (c *HTTPRequest) Token(token string) *HTTPRequest {
	c.token = token
	return c
}

// RequireStatusCode specifies which HTTP status code should be returned by the endpoint.
// This is an optional field, if set the response status code will be compared against this value.
func (c *HTTPRequest) RequireStatusCode(statusCode int) *HTTPRequest {
	c.requireStatusCode = statusCode
	return c
}

// RequireResponseBody pecifies which HTTP response body should be returned by the endpoint.
// This is an optional field, if set the response body will be compared against this value.
func (c *HTTPRequest) RequireResponseBody(responseBody string) *HTTPRequest {
	c.requireResponseBody = responseBody
	return c
}

// QueryParams specifies which the query parameters to be used during the HTTP call.
// This is an optional field.
func (c *HTTPRequest) QueryParams(queryParams map[string]string) *HTTPRequest {
	c.queryParams = queryParams
	return c
}

// Body specifies which the HTTP body to be used during the HTTP call.
// This is an optional field.
func (c *HTTPRequest) Body(requestBody string) *HTTPRequest {
	c.body = requestBody
	return c
}

// ParseResponse specifies if the response from the HTTP endpoint should be parsed according to custom logic.
// See Execute method for more details on the parsing logic.
// This is an optional field.
func (c *HTTPRequest) ParseResponse() *HTTPRequest {
	c.parseResponse = true
	return c
}

// Execute triggers the HTTP request according to all configuration set in the above fields
func (c *HTTPRequest) Execute(t *testing.T) (map[string]interface{}, map[string]interface{}) {
	var reqBody io.Reader
	t.Logf("invoking http request: %s %s", c.method, c.url)

	// set request body if was specified
	if c.body != "" {
		t.Logf("request body: %s", c.body)
		reqBody = strings.NewReader(c.body)
	}
	req, err := http.NewRequest(c.method, c.url, reqBody)
	require.NoError(t, err)

	// set auth token if was specified
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("content-type", "application/json")

	// set query parameters if specified
	if len(c.queryParams) > 0 {
		q := req.URL.Query()
		for key, val := range c.queryParams {
			q.Add(key, val)
		}
		req.URL.RawQuery = q.Encode()
	}

	// close connection after reading response
	req.Close = true
	resp, err := c.client.Do(req) // nolint:bodyclose // see `defer.Close(...)`
	t.Logf("response status code: %d", resp.StatusCode)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotNil(t, body)
	// check for required status code if specified
	if c.requireStatusCode != 0 {
		require.Equal(t, c.requireStatusCode, resp.StatusCode, "unexpected response status with body: %s", body)
	}

	// check for required body if specified
	if c.requireResponseBody != "" {
		value := string(body)
		require.NoError(t, err)

		// Verify JSON response.
		require.Equal(t, c.requireResponseBody, value)
	}

	mp := make(map[string]interface{})
	if len(body) > 0 {
		err = json.Unmarshal(body, &mp)
		require.NoError(t, err)
	}

	if c.parseResponse {
		// Check that the response looks fine
		status, ok := mp["status"].(map[string]interface{})
		require.True(t, ok)
		return mp, status
	}

	// return plain response
	return mp, nil
}
