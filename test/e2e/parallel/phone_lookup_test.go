package parallel

import (
	"fmt"
	"net/http"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Twilio test credentials with magic lookup numbers return deterministic SMS Pumping Risk
// responses at no cost. The API takes country_code and phone_number separately.
// See: https://www.twilio.com/docs/lookup/magic-numbers-for-lookup/testing-sms-pumping-risk-with-magic-numbers
//
// Magic numbers used:
//
//	+441234567890 → high risk, not blocked, score 2
//	+441234567891 → high risk, blocked, score 34
//	+911234567890 → low risk, not blocked, score 2
const (
	twilioMagicPhoneHighRisk        = "1234567890" // +44 prefix → high risk, not blocked
	twilioMagicPhoneHighRiskBlocked = "1234567891" // +44 prefix → high risk, blocked
	usTestPhone                     = "2025550123" // +1 prefix → fictional NANPA 555-01XX range, safe with test credentials
)

func TestPhoneLookupMode(t *testing.T) {
	await := WaitForDeployments(t)
	hostAwait := await.Host()
	route := hostAwait.RegistrationServiceURL

	t.Run("log mode stores phone lookup annotations when lookup succeeds", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeLog))
		userSignup, token := signup(t, hostAwait)

		// when
		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, twilioMagicPhoneHighRiskBlocked), http.StatusNoContent)

		// then
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupDetailsAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
		assert.False(t, states.Rejected(userSignup), "UserSignup should NOT be rejected in log mode")
	})

	t.Run("enabled mode blocks verification for previously rejected signup", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled))
		userSignup, token := signup(t, hostAwait)

		_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetRejected(us, true)
				})
		require.NoError(t, err)

		// when
		responseMap := NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+91", "phone_number":"%s" }`, twilioMagicPhoneHighRisk), http.StatusForbidden).
			UnmarshalMap()

		// then
		require.NotEmpty(t, responseMap)
		assert.Equal(t, "Forbidden", responseMap["status"])

		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name)
		require.NoError(t, err)
		assert.True(t, states.Rejected(userSignup), "UserSignup should remain rejected")
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})

	t.Run("enabled mode skips lookup for US numbers", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().
			Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled).
			Verification().PhoneLookupExcludedCountries([]string{"US", "CA"}))
		userSignup, token := signup(t, hostAwait)

		// when — US is excluded so lookup is never called
		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+1", "phone_number":"%s" }`, usTestPhone), http.StatusNoContent)

		// then
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupDetailsAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})
}
