package sandboxui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// trace starts Playwright tracing for the given browser context and
// saves the trace if the test fails under trace folder
func trace(t *testing.T, context playwright.BrowserContext, testName string) {
	err := context.Tracing().Start(playwright.TracingStartOptions{
		Screenshots: playwright.Bool(true),
		Snapshots:   playwright.Bool(true),
		Sources:     playwright.Bool(true),
	})
	require.NoError(t, err)

	tracePath := filepath.Join(getTraceDirectory(t), fmt.Sprintf("trace-%s.zip", testName))

	t.Cleanup(func() {
		if t.Failed() {
			if err := context.Tracing().Stop(tracePath); err != nil {
				t.Logf("failed to save trace: %v", err)
			} else {
				t.Logf("saved trace to %s", tracePath)
			}
		}
	})
}

func getTraceDirectory(t *testing.T) string {
	dir, err := os.Getwd()
	require.NoError(t, err)

	dirName := "trace"
	getTraceDirectoryPath := filepath.Join(dir, "..", "..", dirName)

	if os.Getenv("RUNNING_IN_CONTAINER") == "true" {
		getTraceDirectoryPath = filepath.Join(os.Getenv("E2E_REPO_PATH"), dirName)
	}

	// Use ARTIFACT_DIR if set (CI environment)
	if artifactDir := os.Getenv("ARTIFACT_DIR"); artifactDir != "" {
		// save trace in the job CI artifact directory
		// artifacts/e2e/test/artifacts/devsandbox-dashboard/trace
		getTraceDirectoryPath = filepath.Join(artifactDir, "devsandbox-dashboard", dirName)
	}

	return getTraceDirectoryPath
}

// handleRecordedVideo manages recorded videos for main pages and popups:
// - On test failure: renames videos from auto-generated IDs to test names (popups include UUID suffix)
// - On test success: removes videos
func handleRecordedVideo(t *testing.T, page playwright.Page, targetVideoPath string) {
	t.Cleanup(func() {
		videoPath, err := page.Video().Path()
		if err != nil || videoPath == "" {
			t.Logf("failed to resolve video path %s: %v", videoPath, err)
		}

		// Handle failed test - rename video
		if t.Failed() {
			// For popup videos, rename with UUID to avoid conflicts when multiple popups exist in the same test
			if strings.Contains(targetVideoPath, "popup") {
				uuid := filepath.Base(videoPath)
				uuid = strings.TrimSuffix(uuid, ".webm")
				if len(uuid) > 8 {
					uuid = uuid[:8] // Truncate to first 8 chars
				}
				targetVideoPath = strings.Replace(targetVideoPath, "popup", fmt.Sprintf("popup-%s", uuid), 1)
			}

			if err := os.Rename(videoPath, targetVideoPath); err != nil {
				t.Logf("failed to rename video %s: %v", videoPath, err)
			}
			return
		}

		// Test passed - remove video
		if err := os.Remove(videoPath); err != nil {
			t.Logf("failed to remove video %s: %v", videoPath, err)
		}
	})
}
