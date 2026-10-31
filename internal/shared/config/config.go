package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	bricksconfig "github.com/cristiano-pacheco/bricks/pkg/config"
)

type Config struct {
	DatabaseHost     string `config:"database_host"`
	DatabasePort     string `config:"database_port"`
	DatabaseName     string `config:"database_name"`
	DatabaseUser     string `config:"database_user"`
	DatabasePassword string `config:"database_password"`
	DatabaseSSLMode  string `config:"database_sslmode"`
	RedisURL         string `config:"redis_url"`
	APIKey           string `config:"api_key"`
	PublicBaseURL    string `config:"public_base_url"`
	HTTPAddr         string `config:"http_addr"`
}

func Load() (Config, error) {
	loaded, err := bricksconfig.New[Config](bricksconfig.WithPath("app"))
	if err != nil {
		return Config{}, err
	}
	cfg := loaded.Get()
	if err = validateDatabase(&cfg); err != nil {
		return Config{}, err
	}
	if err = validateEndpoints(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateDatabase(cfg *Config) error {
	if cfg.DatabaseHost == "" || cfg.DatabaseName == "" || cfg.DatabaseUser == "" || cfg.DatabasePassword == "" {
		return errors.New("database settings are required")
	}
	port, err := strconv.Atoi(cfg.DatabasePort)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("database port must be between 1 and 65535")
	}
	if cfg.DatabaseSSLMode == "" {
		cfg.DatabaseSSLMode = "require"
	}
	switch cfg.DatabaseSSLMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return errors.New("invalid database SSL mode")
	}
	return nil
}

func validateEndpoints(cfg *Config) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("API key is required")
	}
	u, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" {
		return errors.New("public base URL must be an absolute HTTP or HTTPS URL")
	}
	if cfg.RedisURL == "" {
		return errors.New("Redis URL is required") //nolint:staticcheck // Redis is a proper name.
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if _, _, splitErr := net.SplitHostPort(cfg.HTTPAddr); splitErr != nil {
		return errors.New("HTTP address must include a port")
	}
	return nil
}
