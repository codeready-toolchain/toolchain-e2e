package sandboxui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

const (
	TestEnv = "ui-e2e-tests"
	DevEnv  = "dev"
	ProdEnv = "prod"
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

	pw, err := playwright.Run()
	require.NoError(t, err)

	browser := launchBrowser(t, pw)

	opts := playwright.BrowserNewContextOptions{}
	if env == TestEnv {
		opts.IgnoreHttpsErrors = playwright.Bool(true)
	}

	traceDirectory := getTraceDirectory(t)
	opts.RecordVideo = &playwright.RecordVideo{
		Dir: traceDirectory,
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

	login := NewLoginPage(page, env)
	login.Navigate(t, baseURL)

	if env == ProdEnv {
		// handle cookie consent
		// on prod environment, the cookie consent appears after the login page is loaded
		handleCookiesConsent(t, page)
	}

	login.Login(t, username, password)

	// handle cookie consent
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
	// TrustArc renders as a div with a declarative shadow DOM, not a real iframe.
	consent := page.Locator("div[name=\"trustarc_cm\"]")

	err := consent.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})

	if err != nil {
		// no cookie consent appeared, skip
		return
	}

	// TrustArc can show different modals (e.g. full consent vs. simplified); accept whichever button is present.
	agreeProceed := consent.GetByRole("button", playwright.LocatorGetByRoleOptions{
		Name: "Agree and proceed with",
	})

	// to some us states, like texas, it appears the "Cookie Preferences and Opt-Out Rights" modal
	acceptDefault := consent.GetByRole("button", playwright.LocatorGetByRoleOptions{
		Name: "Accept default",
	})
	consentButton := agreeProceed.Or(acceptDefault)

	IsVisible(t, consentButton)

	err = consentButton.Click()
	require.NoError(t, err)

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
