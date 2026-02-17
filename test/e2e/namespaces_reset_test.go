package e2e

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type namespaceResetFeatureIntegrationSuite struct {
	suite.Suite
	wait.Awaitilities
	httpClient http.Client
}

func (s *namespaceResetFeatureIntegrationSuite) SetupSuite() {
	s.Awaitilities = testsupport.WaitForDeployments(s.T())
	s.httpClient = http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // nolint:gosec
			},
		},
	}
}

func TestNamespacesResetE2E(t *testing.T) {
	suite.Run(t, &namespaceResetFeatureIntegrationSuite{})
}

// TestResetNamespaces tests that the "namespace reset" endpoint works as
// expected.
func (s *namespaceResetFeatureIntegrationSuite) TestResetNamespaces() {
	// given
	memberAwaitily := s.Member1()

	// Create a new user signup for the test.
	userUuid, err := uuid.NewUUID()
	require.NoError(s.T(), err, "unable to generate user uuid")

	userOne := testsupport.NewSignupRequest(s.Awaitilities).
		IdentityID(userUuid).
		Username("userone").
		Email("userone@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(s.Member1()).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T())

	// Get the user's "NSTemplateSet" and its provisioned namespaces for the
	// later checks. We are storing the creation timestamp to make sure that
	// it is different from the one of the freshly created namespaces.
	_, nsTemplateSet := space.VerifyResourcesProvisionedForSpace(s.T(), s.Awaitilities, userOne.Space.Name)
	provisionedNamespaces := nsTemplateSet.Status.ProvisionedNamespaces

	namespacesCreationTimestamp := make(map[string]time.Time)
	for _, ns := range provisionedNamespaces {
		namespace, err := memberAwaitily.WaitForNamespaceWithName(s.T(), ns.Name)
		require.NoError(s.T(), err, "unable to fetch namespace")

		namespacesCreationTimestamp[ns.Name] = namespace.CreationTimestamp.Time
	}

	// Create a token and identity to invoke the "reset-namespace" with.
	userIdentity := &commonauth.Identity{
		ID:       userUuid,
		Username: userOne.UserSignup.Status.CompliantUsername,
	}
	claims := []commonauth.ExtraClaim{commonauth.WithEmailClaim(userOne.UserSignup.Spec.IdentityClaims.Email)}
	claims = append(claims, commonauth.WithUserIDClaim(userIdentity.ID.String()))
	claims = append(claims, commonauth.WithAccountIDClaim("999111"))
	claims = append(claims, commonauth.WithGivenNameClaim("John"))
	claims = append(claims, commonauth.WithFamilyNameClaim("Doe"))
	claims = append(claims, commonauth.WithCompanyClaim("Red Hat"))
	claims = append(claims, commonauth.WithAccountNumberClaim("2222"))

	token, err := authsupport.NewTokenFromIdentity(userIdentity, claims...)
	require.NoError(s.T(), err, `ùnable to create the token for the "namespace reset" request`)

	// when
	// Call the endpoint under test.
	testsupport.NewHTTPRequest(s.T()).
		InvokeEndpoint(http.MethodPost, s.Host().RegistrationServiceURL+"/api/v1/reset-namespaces", token, "", http.StatusAccepted)

	// then
	// Wait for the user's namespaces to reach the "terminating" status.
	for _, pns := range provisionedNamespaces {
		_, err := memberAwaitily.WaitForNamespaceInTerminating(s.T(), pns.Name)
		require.NoError(s.T(), err, `unexpected error when waiting for the namespace "%s" to be terminated`, pns.Name)
	}

	// Verify that the namespaces are provisioned again...
	nsTmplSet, err := memberAwaitily.WaitForNSTmplSet(s.T(), userOne.UserSignup.Status.CompliantUsername, wait.UntilNSTemplateSetHasConditions(wait.Provisioned()))
	require.NoError(s.T(), err, `unexpected error when waiting for the "NSTemplateSet" to become "provisioned"`)

	// ... and double check that the namespaces are active.
	for _, namespace := range nsTmplSet.Spec.Namespaces {
		fetchedNamespace, err := memberAwaitily.WaitForNamespace(s.T(), userOne.UserSignup.Status.CompliantUsername, namespace.TemplateRef, nsTmplSet.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(s.T(), err, `unexpected error when waiting for the namespace "%s" to become active`, namespace.TemplateRef)

		timestamp, ok := namespacesCreationTimestamp[fetchedNamespace.Name]
		if !ok {
			require.FailNow(s.T(), "mismatch in the namespace provisioning", `the recreated namespace "%s" was not part of the original provisioned namespaces' list: %#v'`, fetchedNamespace.Namespace, provisionedNamespaces)
		}

		require.NotEqual(s.T(), timestamp, fetchedNamespace.CreationTimestamp, `the namespace "%s" appears to not have been recreated due to having the same creation timestamp as before`, fetchedNamespace.Name)
	}
}
