package sandboxui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ClickAndWaitForPopup(t *testing.T, currentPage playwright.Page, locator playwright.Locator) (playwright.Page, error) {
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

	// Cleanup popup video since test main video includes all flow
	t.Cleanup(func() {
		videoPath, err := popup.Video().Path()
		if err == nil && videoPath != "" {
			if err := os.Remove(videoPath); err != nil {
				t.Logf("failed to remove popup video %s: %v", videoPath, err)
			} else {
				t.Logf("successfully removed popup video: %s", videoPath)
			}
		} else {
			t.Logf("skipped video removal - path empty or error: %s, %v", videoPath, err)
		}
	})

	return popup, nil
}
