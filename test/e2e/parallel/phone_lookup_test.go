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
)

func TestPhoneLookupMode(t *testing.T) {
	await := WaitForDeployments(t)
	hostAwait := await.Host()
	route := hostAwait.RegistrationServiceURL

	t.Run("phoneLookupMode can be set on ToolchainConfig", func(t *testing.T) {
		// Non-default value: CRD default "log" is omitted in spec when set explicitly, so use "enabled" to verify persistence.
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled))
		VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasPhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled))
	})

	t.Run("disabled mode skips phone lookup annotations", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeDisabled))

		userSignup, token := signup(t, hostAwait)
		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, twilioMagicPhoneHighRisk), http.StatusNoContent)

		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupPhoneHashAnnotationKey])
	})

	t.Run("log mode stores phone lookup annotations when lookup succeeds", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeLog))

		userSignup, token := signup(t, hostAwait)
		NewHTTPRequest(t).
			InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token,
				fmt.Sprintf(`{ "country_code":"+44", "phone_number":"%s" }`, twilioMagicPhoneHighRiskBlocked), http.StatusNoContent)

		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey))
		require.NoError(t, err)

		assert.Equal(t, "rejected", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupPhoneHashAnnotationKey])
		assert.Equal(t, "high", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupCarrierRiskAnnotationKey])
		assert.Equal(t, "true", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupNumberBlockedAnnotationKey])
		assert.Equal(t, "34", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupRiskScoreAnnotationKey])
		// verification must proceed in log mode even when lookup result is rejected
		assert.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})

	t.Run("enabled mode blocks verification for previously rejected signup", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled))

		userSignup, token := signup(t, hostAwait)
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
				fmt.Sprintf(`{ "country_code":"+91", "phone_number":"%s" }`, twilioMagicPhoneHighRisk), http.StatusForbidden).
			UnmarshalMap()

		require.NotEmpty(t, responseMap)
		assert.Equal(t, "Forbidden", responseMap["status"])

		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name)
		require.NoError(t, err)
		assert.Equal(t, "rejected", userSignup.Annotations[toolchainv1alpha1.UserSignupPhoneLookupResultAnnotationKey])
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	})

	t.Run("enabled mode skips lookup for US numbers", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.RegistrationService().Verification().PhoneLookupMode(toolchainv1alpha1.PhoneLookupModeEnabled))

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
