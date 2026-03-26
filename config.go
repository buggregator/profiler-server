package profiler

import (
	"time"

	"github.com/roadrunner-server/errors"
)

// Config represents the profiler plugin configuration
type Config struct {
	// HTTP server address to receive XHProf data
	Addr string `mapstructure:"addr"`
	// Maximum request body size in bytes
	MaxRequestSize int64 `mapstructure:"max_request_size"`
	// HTTP read timeout
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	// HTTP write timeout
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// Jobs integration
	Jobs JobsConfig `mapstructure:"jobs"`
}

// JobsConfig configures Jobs plugin integration
type JobsConfig struct {
	Pipeline string `mapstructure:"pipeline"` // Target pipeline in Jobs
	Priority int64  `mapstructure:"priority"` // Default priority for jobs
	Delay    int64  `mapstructure:"delay"`    // Default delay (0 = immediate)
	AutoAck  bool   `mapstructure:"auto_ack"` // Auto-acknowledge jobs
}

// InitDefaults sets default values for configuration
func (c *Config) InitDefaults() error {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:9914"
	}

	if c.MaxRequestSize == 0 {
		c.MaxRequestSize = 50 * 1024 * 1024 // 50MB — profiles can be large
	}

	if c.ReadTimeout == 0 {
		c.ReadTimeout = 60 * time.Second
	}

	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}

	if c.Jobs.Priority == 0 {
		c.Jobs.Priority = 10
	}

	return c.validate()
}

func (c *Config) validate() error {
	const op = errors.Op("profiler_config_validate")

	if c.Addr == "" {
		return errors.E(op, errors.Str("addr is required"))
	}

	if c.Jobs.Pipeline == "" {
		return errors.E(op, errors.Str("jobs.pipeline is required"))
	}

	return nil
}
