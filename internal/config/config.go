package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	LogLevel        string
	ShutdownTimeout time.Duration

	HTTP     HTTPConfig
	Database DatabaseConfig
}

type HTTPConfig struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

func Load() (Config, error) {
	httpAddr, err := requiredString("HTTP_ADDR")
	if err != nil {
		return Config{}, err
	}

	httpReadTimeout, err := requiredDuration("HTTP_READ_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	httpReadHeaderTimeout, err := requiredDuration("HTTP_READ_HEADER_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	httpWriteTimeout, err := requiredDuration("HTTP_WRITE_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	httpIdleTimeout, err := requiredDuration("HTTP_IDLE_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	logLevel, err := requiredString("LOG_LEVEL")
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := requiredDuration("SHUTDOWN_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	databaseURL, err := requiredString("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	maxConns, err := requiredInt32("DATABASE_MAX_CONNS")
	if err != nil {
		return Config{}, err
	}

	minConns, err := requiredInt32("DATABASE_MIN_CONNS")
	if err != nil {
		return Config{}, err
	}

	maxConnLifetime, err := requiredDuration("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return Config{}, err
	}

	connectTimeout, err := requiredDuration("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	queryTimeout, err := requiredDuration("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
		HTTP: HTTPConfig{
			Addr:              httpAddr,
			ReadTimeout:       httpReadTimeout,
			ReadHeaderTimeout: httpReadHeaderTimeout,
			WriteTimeout:      httpWriteTimeout,
			IdleTimeout:       httpIdleTimeout,
		},
		Database: DatabaseConfig{
			URL:             databaseURL,
			MaxConns:        maxConns,
			MinConns:        minConns,
			MaxConnLifetime: maxConnLifetime,
			ConnectTimeout:  connectTimeout,
			QueryTimeout:    queryTimeout,
		},
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func requiredString(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}

	return value, nil
}

func requiredDuration(name string) (time.Duration, error) {
	value, err := requiredString(name)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be duration: %w", name, err)
	}

	return duration, nil
}

func requiredInt32(name string) (int32, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be int32: %w", name, err)
	}

	return int32(parsed), nil
}

func validate(cfg Config) error {
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
	}

	if cfg.HTTP.ReadTimeout <= 0 {
		return fmt.Errorf("HTTP_READ_TIMEOUT must be positive")
	}

	if cfg.HTTP.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("HTTP_READ_HEADER_TIMEOUT must be positive")
	}

	if cfg.HTTP.WriteTimeout <= 0 {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT must be positive")
	}

	if cfg.HTTP.IdleTimeout <= 0 {
		return fmt.Errorf("HTTP_IDLE_TIMEOUT must be positive")
	}

	if cfg.Database.MaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be positive")
	}

	if cfg.Database.MinConns < 0 {
		return fmt.Errorf("DATABASE_MIN_CONNS must be non-negative")
	}

	if cfg.Database.MinConns > cfg.Database.MaxConns {
		return fmt.Errorf("DATABASE_MIN_CONNS must be less than or equal to DATABASE_MAX_CONNS")
	}

	if cfg.Database.MaxConnLifetime <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONN_LIFETIME must be positive")
	}

	if cfg.Database.ConnectTimeout <= 0 {
		return fmt.Errorf("DATABASE_CONNECT_TIMEOUT must be positive")
	}

	if cfg.Database.QueryTimeout <= 0 {
		return fmt.Errorf("DATABASE_QUERY_TIMEOUT must be positive")
	}

	return nil
}
