package setup

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestSetupMigration(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)

	runner := migration.SetupMigrationRunner{
		T:            t,
		Awaitilities: awaitilities,
		WithCleanup:  false,
	}

	runner.Run()

}
