package setup

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestSetupMigration(t *testing.T) {
	// given
	// set env var to skip the mutating webhook check on migration setup temporarily since the old deployment
	// will deploy the webhooks with the old configuration but the tests will be expecting the new configuration
	// This should be removed after the PR is merged
	t.Setenv("skip-mutating-webhook-check-on-setup", "true")
	awaitilities := WaitForDeployments(t)

	runner := migration.SetupMigrationRunner{
		Awaitilities: awaitilities,
		WithCleanup:  false,
	}

	runner.Run(t)

}
