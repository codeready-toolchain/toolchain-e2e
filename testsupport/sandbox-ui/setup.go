package sandboxui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

var (
	setupOnce sync.Once

	UIE2ETestsEnv = "ui-e2e-tests"
)

func Setup(t *testing.T, testName string) playwright.Page {
	setupOnce.Do(func() {
		dir, err := os.Getwd()
		require.NoError(t, err)

		viper.SetConfigFile(filepath.Join(dir, "../../../testsupport/sandbox-ui/.env"))
		err = viper.ReadInConfig()
		require.NoError(t, err)
		viper.AutomaticEnv()
	})

	env := viper.GetString("ENVIRONMENT")
	baseURL := viper.GetString("BASE_URL")
	username := viper.GetString("SSO_USERNAME")
	password := viper.GetString("SSO_PASSWORD")

	pw, err := playwright.Run()
	require.NoError(t, err)

	browser := launchBrowser(t, pw)

	opts := playwright.BrowserNewContextOptions{}
	if env == UIE2ETestsEnv {
		opts.IgnoreHttpsErrors = playwright.Bool(true)
	}

	context, err := browser.NewContext(opts)
	require.NoError(t, err)

	// save trace
	trace(t, context, testName)

	page, err := context.NewPage()
	require.NoError(t, err)

	login := NewLoginPage(page, env)
	login.Navigate(t, baseURL)
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
	// wait for the iframe to appear
	iframe := page.Locator("iframe[name=\"trustarc_cm\"]")

	// check if iframe exists
	err := iframe.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})

	if err != nil {
		// no cookie consent appeared, skip
		return
	}

	// get the content frame
	contentFrame := iframe.ContentFrame()
	require.NotNil(t, contentFrame)

	// find the agree button
	agreeButton := contentFrame.GetByRole("button", playwright.FrameLocatorGetByRoleOptions{
		Name: "Agree and proceed with",
	})

	err = agreeButton.Click()
	require.NoError(t, err)

	// wait for iframe to disappear (indicates cookie acceptance complete)
	err = iframe.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateDetached,
	})
	require.NoError(t, err)

	// wait for page to stabilize
	err = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	require.NoError(t, err)
}
