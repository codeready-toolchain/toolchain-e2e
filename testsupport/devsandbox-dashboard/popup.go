package devsandboxdashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ClickAndWaitForPopup(currentPage playwright.Page, locator playwright.Locator) (playwright.Page, error) {
	var popup playwright.Page
	var err error

	pollErr := wait.PollUntilContextTimeout(context.TODO(), time.Millisecond, 1*time.Minute, true, func(ctx context.Context) (bool, error) {
		popup, err = currentPage.ExpectPopup(func() error {
			return locator.Click()
		})

		if err != nil {
			// only retry on timeout-like errors
			if strings.Contains(err.Error(), "Timeout") || popup == nil {
				return false, nil
			}
			return false, err // stop polling on fatal errors
		}
		return true, nil
	})

	if pollErr != nil || popup == nil {
		return nil, fmt.Errorf("popup did not appear in time: %w", pollErr)
	}

	waitErr := popup.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateLoad,
		Timeout: playwright.Float(60000), // 1 minute
	})
	if waitErr != nil {
		return nil, fmt.Errorf("popup did not finish loading: %w", waitErr)
	}
	return popup, nil
}
