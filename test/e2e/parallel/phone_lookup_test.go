package parallel

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhoneLookupMode(t *testing.T) {
	await := WaitForDeployments(t)
	hostAwait := await.Host()
	route := hostAwait.RegistrationServiceURL

	t.Run("phoneLookupMode can be set on ToolchainConfig", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode("log"))
		VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasPhoneLookupMode("log"))

		config := hostAwait.GetToolchainConfig(t)
		require.NotNil(t, config.Spec.Host.RegistrationService.Verification.PhoneLookupMode)
		assert.Equal(t, "log", *config.Spec.Host.RegistrationService.Verification.PhoneLookupMode)
	})

	t.Run("disabled mode skips phone lookup annotations", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode("disabled"))

		userSignup, token := signup(t, hostAwait)
		phoneNumber := uniqueUKPhoneNumber()

		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, phoneNumber), http.StatusNoContent)

		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupPhoneHashAnnotationKey])
	})

	t.Run("log mode stores phone lookup annotations when lookup succeeds", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode("log"))

		userSignup, token := signup(t, hostAwait)
		phoneNumber := uniqueUKPhoneNumber()

		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, phoneNumber), http.StatusNoContent)

		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		lookupResult := userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey]
		if lookupResult == "" {
			t.Log("Twilio Lookup did not return a result (fail-open); skipping lookup annotation assertions")
			return
		}

		assert.Contains(t, []string{"allowed", "rejected"}, lookupResult)
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupPhoneHashAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupCarrierRiskAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupNumberBlockedAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupRiskScoreAnnotationKey])
		// verification must proceed in log mode even when lookup result is rejected
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})

	t.Run("enabled mode blocks verification for previously rejected signup", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode("enabled"))

		userSignup, token := signup(t, hostAwait)
		phoneNumber := uniqueUKPhoneNumber()

		_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					if us.Annotations == nil {
						us.Annotations = map[string]string{}
					}
					us.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey] = "rejected"
				})
		require.NoError(t, err)

		responseMap := NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, phoneNumber), http.StatusForbidden).
			UnmarshalMap()

		require.NotEmpty(t, responseMap)
		assert.Equal(t, "Forbidden", responseMap["status"])

		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name)
		require.NoError(t, err)
		assert.Equal(t, "rejected", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})

	t.Run("enabled mode skips lookup for US numbers", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode("enabled"))

		userSignup, token := signup(t, hostAwait)
		phoneNumber := strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")[:10]

		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+1", "phone_number":"%s" }`, phoneNumber), http.StatusNoContent)

		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})
}

func uniqueUKPhoneNumber() string {
	// UK mobile numbers are 10 digits after country code; use a unique suffix to avoid phone-in-use conflicts
	suffix := strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")[:10]
	return "77009" + suffix[len(suffix)-5:]
}
