package config

import (
	"fmt"
	"os"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	LogLevel        string        `env:"LOG_LEVEL" env-required:"true" validate:"oneof=debug info warn error"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" env-required:"true" validate:"gt=0"`

	HTTP     HTTPConfig
	Database DatabaseConfig
}

type HTTPConfig struct {
	Addr              string        `env:"HTTP_ADDR" env-required:"true" validate:"required"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" env-required:"true" validate:"gt=0"`
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" env-required:"true" validate:"gt=0"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" env-required:"true" validate:"gt=0"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" env-required:"true" validate:"gt=0"`
}

type DatabaseConfig struct {
	URL             string        `env:"DATABASE_URL" env-required:"true" validate:"required"`
	MaxConns        int32         `env:"DATABASE_MAX_CONNS" env-required:"true" validate:"gt=0"`
	MinConns        int32         `env:"DATABASE_MIN_CONNS" env-required:"true" validate:"gte=0,ltefield=MaxConns"`
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" env-required:"true" validate:"gt=0"`
	ConnectTimeout  time.Duration `env:"DATABASE_CONNECT_TIMEOUT" env-required:"true" validate:"gt=0"`
	QueryTimeout    time.Duration `env:"DATABASE_QUERY_TIMEOUT" env-required:"true" validate:"gt=0"`
}

func Load() (Config, error) {
	var cfg Config

	if err := readConfig(&cfg); err != nil {
		return Config{}, err
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func readConfig(cfg *Config) error {
	if _, err := os.Stat(".env"); err == nil {
		if err := cleanenv.ReadConfig(".env", cfg); err != nil {
			return fmt.Errorf("read .env config: %w", err)
		}

		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check .env file: %w", err)
	}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		return fmt.Errorf("read env config: %w", err)
	}

	return nil
}

func validateConfig(cfg Config) error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	return nil
}
