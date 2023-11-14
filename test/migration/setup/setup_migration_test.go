package setup

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestSetupMigration(t *testing.T) {
	// given
	awaitilities := WaitForOperators(t)

	runner := migration.SetupMigrationRunner{
		Awaitilities: awaitilities,
		WithCleanup:  false,
	}

	runner.Run(t)

}
