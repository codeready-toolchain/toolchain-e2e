package sandboxui

import (
	"regexp"
	"testing"

	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/sandbox-ui"
	"github.com/playwright-community/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHomepage tests the homepage layout and welcome text
// when the user accesses the Developer Sandbox UI for the first time
func TestHomepage(t *testing.T) {
	page := sandboxui.Setup(t, "test-homepage")

	homeLink := page.Locator("a").Filter(playwright.LocatorFilterOptions{
		HasText: "Home",
	})
	isVisible, err := homeLink.IsVisible()
	require.NoError(t, err)
	assert.True(t, isVisible)

	article := page.GetByRole("article")

	// check heading
	heading := article.GetByRole("heading", playwright.LocatorGetByRoleOptions{
		Level: playwright.Int(1),
	})
	headingText, err := heading.TextContent()
	require.NoError(t, err)
	assert.Equal(t, "Try Red Hat products", headingText)

	// check paragraphs
	require.NotNil(t, article.GetByText("Explore, experiment, and see what's possible"))
	require.NotNil(t, article.GetByText(regexp.MustCompile(`Click on "Try it" to initiate your free, no commitment \d+-day trial\.`)))

	// check image
	img := page.GetByRole("img", playwright.PageGetByRoleOptions{
		Name: "Red Hat Trial",
	})
	err = img.WaitFor()
	require.NoError(t, err)
}

// TestSignup tests the signup flow (automatically approved)
// when the user clicks on "Try it" for the first time
func TestSignup(t *testing.T) {
	page := sandboxui.Setup(t, "test-signup")

	article := page.GetByRole("article")
	loadingIcon := page.Locator("svg.v5-MuiCircularProgress-svg").First()

	card := page.GetByText("OpenShift Comprehensive cloud")
	isVisible(t, card)

	// check cards
	cardsToCheck := []string{
		"OpenShift AI Scalable AI and",
		"Dev Spaces Cloud Development",
		"Ansible Automation Platform Scalable, centralized automation solution Available",
		"OpenShift Virtualization Migrate traditional VM workloads to OpenShift Unified",
	}
	for _, text := range cardsToCheck {
		elem := page.GetByText(text)
		isVisible(t, elem)
	}

	// check heading
	heading := article.GetByRole("heading", playwright.LocatorGetByRoleOptions{
		Level: playwright.Int(1),
	})
	headingText, err := heading.TextContent()
	require.NoError(t, err)
	assert.Equal(t, "Try Red Hat products", headingText)

	// check paragraph
	require.NotNil(t, article.GetByText("Click on \"Try it\" to initiate your free, no commitment 30-day trial."))

	// click the "Try it" button in the card
	tryItButton := card.Locator("button", playwright.LocatorLocatorOptions{
		HasText: "Try it",
	})
	err = tryItButton.Click()
	require.NoError(t, err)

	// wait for loading icon to disappear
	err = loadingIcon.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateHidden})
	require.NoError(t, err)

	// verify texts in article header after click
	updatedText, err := article.TextContent()
	require.NoError(t, err)

	assert.Contains(t, updatedText, "Welcome")
	assert.Contains(t, updatedText, "Your free trial expires in 30 days")
}

// TestDevSandbox tests the access to Openshift after the user is signed up
func TestDevSandbox(t *testing.T) {
	page := sandboxui.Setup(t, "test-devsandbox")
	env := viper.GetString("ENVIRONMENT")

	imgName := "Red Hat OpenShift Service on"
	logMessage := "Log in with…"
	if env == "e2e-tests" {
		imgName = "Red Hat OpenShift"
		logMessage = "Log in with"
	}

	// click on the 'Try it' button
	tryItBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Try it",
	}).First()

	err := tryItBtn.WaitFor()
	require.NoError(t, err)

	// open the article in a new popup and wait for it to fully load
	devSandboxPage, err := sandboxui.ClickAndWaitForPopup(page, tryItBtn)
	require.NoError(t, err)

	// wait for a specific element that should be visible after the page is fully loaded
	img := devSandboxPage.GetByRole("img", playwright.PageGetByRoleOptions{
		Name: imgName,
	})
	err = img.WaitFor()
	require.NoError(t, err)

	h := devSandboxPage.GetByRole("heading", playwright.PageGetByRoleOptions{})
	hText, err := h.TextContent()
	require.NoError(t, err)
	require.Contains(t, hText, logMessage)

	list := devSandboxPage.GetByRole("list", playwright.PageGetByRoleOptions{})
	listText, err := list.TextContent()
	require.NoError(t, err)
	require.Contains(t, listText, "DevSandbox")

	require.NoError(t, devSandboxPage.Close())
}

func isVisible(t *testing.T, elem playwright.Locator) {
	visible, err := elem.IsVisible()
	require.NoError(t, err)
	assert.True(t, visible)
}
