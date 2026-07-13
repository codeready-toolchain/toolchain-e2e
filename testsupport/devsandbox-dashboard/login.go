package sandboxui

import (
	"fmt"
	"log"
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

	err = lp.LoginUsernameLoc.Fill(loginUsername)
	require.NoError(t, err)

	// Ensure Fill actually stuck before clicking Next (Mode A: empty field + validation error).
	filled, err := lp.LoginUsernameLoc.InputValue()
	require.NoError(t, err)
	require.Equal(t, loginUsername, filled, "username field value after Fill did not match SSO_USERNAME")

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
			return fmt.Errorf("SSO rejected username step: %q (username field was empty or Fill did not stick; see screenshot after-next)", ssoLoginRequiredText)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for password field after Next (also did not see %q)", ssoLoginRequiredText)
}

// waitForDashboardOrProfileGate waits for the dashboard header, or handles/fails
// on the intermittent SSO "We need a little more information" profile page.
func (lp *LoginPage) waitForDashboardOrProfileGate(t *testing.T) error {
	t.Helper()
	deadline := time.Now().Add(time.Duration(loginStepTimeoutMs) * time.Millisecond)
	profileGate := lp.Page.GetByText(ssoProfileGateText)
	profileSubmitAttempted := false

	for time.Now().Before(deadline) {
		if visible, _ := lp.Header.IsVisible(); visible {
			return nil
		}
		if visible, _ := profileGate.IsVisible(); visible {
			if !profileSubmitAttempted {
				if tryCompleteSSOProfile(t, lp.Page) {
					profileSubmitAttempted = true
					time.Sleep(500 * time.Millisecond)
					continue
				}
				return fmt.Errorf("SSO blocked login with profile completion page %q for the test user; complete the account profile offline or extend the login helper (see screenshot after-login)", ssoProfileGateText)
			}
			// Submit already attempted; keep waiting for navigation away from the gate.
		}
		time.Sleep(200 * time.Millisecond)
	}
	if visible, _ := profileGate.IsVisible(); visible {
		return fmt.Errorf("SSO profile completion page %q still present after login; complete the account profile offline (see screenshot after-login)", ssoProfileGateText)
	}
	return fmt.Errorf("timed out waiting for dashboard header after login")
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

// dumpLoginFailure writes a screenshot and logs URL + a short page text snippet
// so CI failures are diagnosable without downloading the webm.
func dumpLoginFailure(t *testing.T, page playwright.Page, label string) {
	t.Helper()

	url := page.URL()
	t.Logf("login failure (%s) page URL: %s", label, url)

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
