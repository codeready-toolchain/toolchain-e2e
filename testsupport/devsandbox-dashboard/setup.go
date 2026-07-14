package sandboxui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/mxschmitt/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

const (
	TestEnv = "ui-e2e-tests"
	DevEnv  = "dev"
	ProdEnv = "prod"

	// TrustArc cookie consent container (declarative shadow DOM, not an iframe).
	trustArcConsentLocator = `div[name="trustarc_cm"]`
)

var (
	setupOnce sync.Once
)

func LoadConfig(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	configPath := filepath.Join(filepath.Dir(filename), ".env")
	viper.SetConfigFile(configPath)

	err := viper.ReadInConfig()
	require.NoError(t, err)

	viper.AutomaticEnv()
}

func Setup(t *testing.T, testName string) playwright.Page {
	setupOnce.Do(func() {
		LoadConfig(t)
	})

	env := viper.GetString("ENVIRONMENT")
	baseURL := viper.GetString("BASE_URL")
	username := viper.GetString("SSO_USERNAME")
	password := viper.GetString("SSO_PASSWORD")
	require.NotEmpty(t, baseURL, "BASE_URL must be set")
	require.NotEmpty(t, username, "SSO_USERNAME must be set (check sandbox-test-kc / sandbox secrets mount)")
	require.NotEmpty(t, password, "SSO_PASSWORD must be set (check sandbox-test-kc / sandbox secrets mount)")

	pw, err := playwright.Run()
	require.NoError(t, err)

	browser := launchBrowser(t, pw)

	opts := playwright.BrowserNewContextOptions{}
	if env == TestEnv {
		opts.IgnoreHttpsErrors = playwright.Bool(true)
	}

	traceDirectory := getTraceDirectory(t)
	opts.RecordVideo = &playwright.RecordVideo{
		Dir: playwright.String(traceDirectory),
	}

	context, err := browser.NewContext(opts)
	require.NoError(t, err)

	// save trace only if not running in CI
	// we do not want to expose sensitive information in CI
	if os.Getenv("ARTIFACT_DIR") == "" { // not CI environment
		trace(t, context, testName)
	}

	page, err := context.NewPage()
	require.NoError(t, err)

	handleRecordedVideo(t, page, filepath.Join(traceDirectory, fmt.Sprintf("%s.webm", testName)))

	// Dismiss TrustArc whenever it appears during navigate/login. Prod videos show the
	// banner arriving mid-Fill and SSO remounting, which clears the username field.
	installCookieDismissHandler(t, page)

	login := NewLoginPage(page, env)
	login.Navigate(t, baseURL)

	if env == ProdEnv {
		// Best-effort dismiss if already visible before login starts.
		handleCookiesConsent(t, page)
	}

	login.Login(t, username, password)

	// handle cookie consent after landing on the dashboard
	handleCookiesConsent(t, page)

	return page
}

func launchBrowser(t *testing.T, pw *playwright.Playwright) playwright.Browser {
	var browser playwright.Browser
	var err error

	browserName := viper.GetString("BROWSER")

	switch browserName {
	case "chromium":
		browser, err = pw.Chromium.Launch()
	case "firefox":
		browser, err = pw.Firefox.Launch()
	case "webkit":
		browser, err = pw.WebKit.Launch()
	default:
		t.Fatalf("unsupported browser: %s", browserName)
	}
	require.NoError(t, err)

	return browser
}

func handleCookiesConsent(t *testing.T, page playwright.Page) {
	// Short wait: consent is optional and must not burn 30s on every run when absent.
	const consentWaitMs = 10000.0

	// TrustArc renders as a div with a declarative shadow DOM, not a real iframe.
	consent := page.Locator(trustArcConsentLocator)

	err := consent.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(consentWaitMs),
	})

	if err != nil {
		// Fallback: banner may not use trustarc_cm naming.
		_ = clickCookieAcceptIfPresent(page)
		return
	}

	require.True(t, clickCookieAcceptIfPresent(page), "TrustArc consent visible but no known accept button found")

	// wait for the consent banner to disappear
	err = consent.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateDetached,
	})
	require.NoError(t, err)

	// wait for page to stabilize
	err = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	require.NoError(t, err)
}

// knownCookieAcceptButtons covers TrustArc variants seen in prod CI videos.
var knownCookieAcceptButtons = []string{
	"Accept default",
	"Accept all",
	"Agree and proceed with",
	"Required Cookies only",
}

// clickCookieAcceptIfPresent clicks the first visible known TrustArc accept button.
func clickCookieAcceptIfPresent(page playwright.Page) bool {
	consent := page.Locator(trustArcConsentLocator)
	for _, name := range knownCookieAcceptButtons {
		// Prefer buttons scoped to the TrustArc container (shadow DOM), then page-wide.
		for _, btn := range []playwright.Locator{
			consent.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: name}),
			page.GetByRole("button", playwright.PageGetByRoleOptions{Name: name}),
		} {
			if visible, _ := btn.IsVisible(); visible {
				if err := btn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err == nil {
					return true
				}
			}
		}
	}
	return false
}

// installCookieDismissHandler auto-dismisses TrustArc overlays that appear mid-login.
// Without this, Fill(username) can stick briefly then be cleared when SSO remounts
// after the cookie modal (see ci-daily-prod failure videos).
func installCookieDismissHandler(t *testing.T, page playwright.Page) {
	t.Helper()
	consent := page.Locator(trustArcConsentLocator)
	err := page.AddLocatorHandler(consent, func(_ playwright.Locator) {
		_ = clickCookieAcceptIfPresent(page)
	})
	require.NoError(t, err)
}
