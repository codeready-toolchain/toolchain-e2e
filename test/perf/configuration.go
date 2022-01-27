package perf

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// host-operator constants
const (
	// UserCount the total number of users to provision. Must be a multiple of `UserBatchSize`
	UserCount = "user.count"

	defaultUserCount = 1000

	// UserBatchSize the size of each user provisioning batch.
	UserBatchSize = "user.batch.size"

	defaultUserBatchSize = 100

	// UserBatchPause the duration (in seconds) after each batch of users was fully provisioned
	UserBatchPause = "user.batch.pause"

	defaultUserBatchPause = 5 // seconds
)

// Configuration encapsulates the Viper configuration registry which stores the
// configuration data in-memory.
type Configuration struct {
	v *viper.Viper
}

// NewConfiguration initializes the configuration from the env vars
func NewConfiguration() Configuration {
	c := Configuration{
		v: viper.New(),
	}
	c.v.AutomaticEnv()
	c.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.v.SetTypeByDefaultValue(true)
	c.setConfigDefaults()
	return c
}

func (c Configuration) setConfigDefaults() {
	c.v.SetTypeByDefaultValue(true)
	c.v.SetDefault(UserBatchSize, defaultUserBatchSize)
	c.v.SetDefault(UserCount, defaultUserCount)
	c.v.SetDefault(UserBatchPause, defaultUserBatchPause)
}

// GetUserCount returns the configured `user.count` (or its default value)
func (c Configuration) GetUserCount() int {
	return c.v.GetInt(UserCount)
}

// GetUserBatchSize returns the configured `user.batch.size` (or its default value)
func (c Configuration) GetUserBatchSize() int {
	return c.v.GetInt(UserBatchSize)
}

// GetUserBatchPause returns the configured `user.batch.pause` (or its default value)
func (c Configuration) GetUserBatchPause() time.Duration {
	return time.Duration(c.v.GetInt(UserBatchPause)) * time.Second
}
