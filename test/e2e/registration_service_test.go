package e2e

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestRegistrationService(t *testing.T) {
	suite.Run(t, &registrationServiceTestSuite{})
}

type registrationServiceTestSuite struct {
	suite.Suite
	namespace  string
	route      string
	testCtx    *framework.TestCtx
	awaitility *wait.Awaitility
}

func (s *registrationServiceTestSuite) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.testCtx, s.awaitility = doubles.WaitForDeployments(s.T(), userSignupList)
	s.namespace = s.awaitility.RegistrationServiceNs
	s.route = s.awaitility.RegistrationServiceURL
}

func (s *registrationServiceTestSuite) TestLandingPageReachable() {
	// just make sure that the landing page is reachable
	req, err := http.NewRequest("GET", s.route, nil)
	require.NoError(s.T(), err)
	client := &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
}
