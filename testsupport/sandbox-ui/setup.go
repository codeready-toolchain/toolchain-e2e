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

	browser := lunchBrowser(t, pw)

	opts := playwright.BrowserNewContextOptions{}
	if env == "e2e-tests" {
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

	return page
}

func lunchBrowser(t *testing.T, pw *playwright.Playwright) playwright.Browser {
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
