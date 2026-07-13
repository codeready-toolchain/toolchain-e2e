package sandboxui

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	loginStepTimeoutMs = 60000.0
	// SSO validation error shown when Next is clicked with an empty username.
	ssoLoginRequiredText = "Red Hat login is required"
	// Intermittent SSO gate after successful password login.
	ssoProfileGateText = "We need a little more information"
)

type LoginPage struct {
	Page             playwright.Page
	LoginUsernameLoc playwright.Locator
	LoginPwLoc       playwright.Locator
	NextBtn          playwright.Locator
	LoginBtn         playwright.Locator
	Header           playwright.Locator
	Env              string
}

func NewLoginPage(page playwright.Page, environment string) *LoginPage {
	lp := &LoginPage{
		Page: page,
		Env:  environment,
	}

	switch environment {
	case DevEnv, ProdEnv:
		lp.LoginUsernameLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Red Hat login",
		})
		lp.LoginPwLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Password",
		})
		lp.NextBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Next",
		})
		lp.LoginBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Log in",
		})
	case TestEnv:
		lp.LoginUsernameLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Username or email",
		})
		lp.LoginPwLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Password",
		})
		lp.LoginBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Sign in",
		})
	default:
		log.Fatalf("unsupported environment: %s", environment)
	}

	lp.Header = page.Locator("header")

	return lp
}

func (lp *LoginPage) Navigate(t *testing.T, url string) {
	_, err := lp.Page.Goto(url, playwright.PageGotoOptions{
		Timeout: playwright.Float(loginStepTimeoutMs),
	})
	require.NoError(t, err)
}

func (lp *LoginPage) Login(t *testing.T, loginUsername, loginPw string) {
	require.NotEmpty(t, loginUsername, "SSO_USERNAME must be set; empty username causes Next to fail with 'Red Hat login is required'")
	require.NotEmpty(t, loginPw, "SSO_PASSWORD must be set")

	err := lp.LoginUsernameLoc.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(loginStepTimeoutMs),
	})
	require.NoError(t, err, "username field not visible")

	// Cookie/SSO remount can clear the field right after Fill (prod CI videos show
	// username briefly then empty before Next). Retry until the value sticks.
	lp.fillUsernameStable(t, loginUsername)

	if lp.Env == DevEnv || lp.Env == ProdEnv {
		err := lp.NextBtn.Click()
		require.NoError(t, err)

		if err := lp.waitForPasswordOrSSOError(t); err != nil {
			dumpLoginFailure(t, lp.Page, "after-next")
			require.NoError(t, err)
		}
	}

	err = lp.LoginPwLoc.Fill(loginPw)
	require.NoError(t, err)

	err = lp.LoginBtn.Click()
	require.NoError(t, err)

	if err := lp.waitForDashboardOrProfileGate(t); err != nil {
		dumpLoginFailure(t, lp.Page, "after-login")
		require.NoError(t, err)
	}

	text, err := lp.Header.TextContent()
	require.NoError(t, err)
	assert.Contains(t, text, "Developer Sandbox")
}

// fillUsernameStable fills the username and re-fills if SSO/cookie remount clears it.
func (lp *LoginPage) fillUsernameStable(t *testing.T, loginUsername string) {
	t.Helper()
	deadline := time.Now().Add(time.Duration(loginStepTimeoutMs) * time.Millisecond)
	const settle = 400 * time.Millisecond

	for time.Now().Before(deadline) {
		// Dismiss cookie if it appeared outside the locator handler path.
		_ = clickCookieAcceptIfPresent(lp.Page)

		err := lp.LoginUsernameLoc.Fill(loginUsername)
		require.NoError(t, err)

		time.Sleep(settle)

		filled, err := lp.LoginUsernameLoc.InputValue()
		require.NoError(t, err)
		if filled != loginUsername {
			t.Logf("username cleared after Fill (got %q); retrying after cookie/SSO settle", filled)
			continue
		}

		// Confirm it remains after another short settle (covers late remount).
		time.Sleep(settle)
		filled, err = lp.LoginUsernameLoc.InputValue()
		require.NoError(t, err)
		if filled == loginUsername {
			return
		}
		t.Logf("username cleared on second settle (got %q); retrying", filled)
	}

	require.Fail(t, "username would not stick in the Red Hat login field before Next")
}

// waitForPasswordOrSSOError waits until the password field is visible, or fails
// immediately if SSO shows a username validation error (empty/unfilled login).
func (lp *LoginPage) waitForPasswordOrSSOError(t *testing.T) error {
	t.Helper()
	deadline := time.Now().Add(time.Duration(loginStepTimeoutMs) * time.Millisecond)
	loginRequired := lp.Page.GetByText(ssoLoginRequiredText)

	for time.Now().Before(deadline) {
		if visible, _ := lp.LoginPwLoc.IsVisible(); visible {
			return nil
		}
		if visible, _ := loginRequired.IsVisible(); visible {
			return fmt.Errorf("SSO rejected username step: %q (username field was empty or Fill did not stick; see failure diagnostics/video)", ssoLoginRequiredText)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for password field after Next (also did not see %q)", ssoLoginRequiredText)
}

// waitForDashboardOrProfileGate waits for the dashboard, or handles/fails on the
// intermittent SSO "We need a little more information" profile page.
func (lp *LoginPage) waitForDashboardOrProfileGate(t *testing.T) error {
	t.Helper()
	deadline := time.Now().Add(time.Duration(loginStepTimeoutMs) * time.Millisecond)
	profileGate := lp.Page.GetByText(ssoProfileGateText)
	// Prefer dashboard-specific text over a generic <header>, which SSO pages may also have.
	dashboardMarker := lp.Page.GetByText("Developer Sandbox")
	profileSubmitAttempted := false

	for time.Now().Before(deadline) {
		// Check profile gate before dashboard markers so an SSO <header> cannot mask it.
		if visible, _ := profileGate.IsVisible(); visible {
			if !profileSubmitAttempted {
				if tryCompleteSSOProfile(t, lp.Page) {
					profileSubmitAttempted = true
					time.Sleep(500 * time.Millisecond)
					continue
				}
				return fmt.Errorf("SSO blocked login with profile completion page %q for the test user; complete the account profile offline or extend the login helper (see failure diagnostics/video)", ssoProfileGateText)
			}
			// Submit already attempted; keep waiting for navigation away from the gate.
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if visible, _ := dashboardMarker.IsVisible(); visible {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if visible, _ := profileGate.IsVisible(); visible {
		return fmt.Errorf("SSO profile completion page %q still present after login; complete the account profile offline (see failure diagnostics/video)", ssoProfileGateText)
	}
	return fmt.Errorf("timed out waiting for dashboard after login")
}

// tryCompleteSSOProfile clicks a common continue/submit control if present on the
// profile gate. Returns true if a click was attempted.
func tryCompleteSSOProfile(t *testing.T, page playwright.Page) bool {
	t.Helper()
	for _, name := range []string{"Confirm", "Continue", "Submit", "Next", "Save"} {
		btn := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: name})
		if visible, _ := btn.IsVisible(); visible {
			t.Logf("SSO profile gate detected; clicking %q", name)
			if err := btn.Click(); err != nil {
				t.Logf("failed to click profile gate button %q: %v", name, err)
				return false
			}
			return true
		}
	}
	return false
}

// dumpLoginFailure logs a redacted URL. Page text and screenshots are only
// captured outside CI, matching Setup's policy of not retaining SSO-sensitive
// Playwright traces when ARTIFACT_DIR is set. Failure videos may still be
// retained separately by the video recorder.
func dumpLoginFailure(t *testing.T, page playwright.Page, label string) {
	t.Helper()

	t.Logf("login failure (%s) page URL: %s", label, redactURL(page.URL()))

	// In CI, avoid logging SSO page body or writing screenshots that can include
	// account details. Local runs keep full diagnostics for debugging.
	if os.Getenv("ARTIFACT_DIR") != "" {
		t.Logf("login failure (%s): skipping page text/screenshot in CI to avoid SSO data in artifacts", label)
		return
	}

	if body, err := page.Locator("body").InnerText(); err == nil {
		snippet := strings.Join(strings.Fields(body), " ")
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		t.Logf("login failure (%s) page text: %s", label, snippet)
	}

	dir := getTraceDirectory(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("failed to create screenshot dir %s: %v", dir, err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("login-failure-%s.png", label))
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(path),
	}); err != nil {
		t.Logf("failed to save login failure screenshot: %v", err)
		return
	}
	t.Logf("saved login failure screenshot to %s", path)
}

// redactURL returns scheme://host/path only, stripping query and fragment that
// may contain OAuth/SSO parameters.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "<unparseable>"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
