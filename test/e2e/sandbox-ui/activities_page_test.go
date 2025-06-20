package sandboxui

import (
	"strings"
	"testing"

	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/sandbox-ui"
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
	err := activitiesLink.Click()
	require.NoError(t, err)

	h3 := page.Locator("h3")
	text, err := h3.TextContent()
	require.NoError(t, err)
	assert.Contains(t, text, "Featured")

	// locate all article cards
	cards := page.GetByRole("heading", playwright.PageGetByRoleOptions{
		Level: playwright.Int(5),
	})

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
