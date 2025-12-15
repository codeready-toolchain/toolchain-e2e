package sandboxui

import (
	"strings"
	"testing"

	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/devsandbox-dashboard"
	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivitiesPage opens and verifies that activities cards links are not broken
func TestActivitiesPage(t *testing.T) {
	page := sandboxui.Setup(t, "test-activities-page")

	// navigate to 'Activities' link
	activitiesLink := page.GetByRole("link", playwright.PageGetByRoleOptions{
		Name: "Activities",
	})
	sandboxui.IsVisible(t, activitiesLink)
	err := activitiesLink.Click()
	require.NoError(t, err)

	// wait for page to load
	err = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	require.NoError(t, err)

	featuredHeading := page.GetByRole("heading", playwright.PageGetByRoleOptions{
		Name: "Featured",
	})
	text, err := featuredHeading.TextContent()
	require.NoError(t, err)
	assert.Contains(t, text, "Featured")

	// locate all article cards
	cards := page.GetByRole("heading", playwright.PageGetByRoleOptions{
		Level: playwright.Int(5),
	})

	//wait for the 6th card (index 5) to appear - if it's there, all 6 are loaded
	err = cards.Nth(5).WaitFor()
	require.NoError(t, err)

	noOfCards, err := cards.Count()
	require.NoError(t, err)
	assert.Equal(t, 6, noOfCards)

	for i := 0; i < noOfCards; i++ {
		article := cards.Nth(i)

		// extract the title of each article card
		articleTitle, err := article.TextContent()
		require.NoError(t, err)

		// open the article in a new popup and wait for it to fully load
		popup, err := sandboxui.ClickAndWaitForPopup(page, article)
		require.NoError(t, err)

		// assert the popup heading matches the expected article title
		h1 := popup.Locator("h1")
		h1Text, err := h1.TextContent()
		require.NoError(t, err)
		// clean text since it returns this format: Overview:\n\t\t\t\t\t\t
		h1Text = strings.ReplaceAll(h1Text, "\n", " ")
		h1Text = strings.ReplaceAll(h1Text, "\t", "")
		assert.Contains(t, h1Text, "Overview: "+articleTitle)

		err = popup.Close()
		require.NoError(t, err)
	}
}
