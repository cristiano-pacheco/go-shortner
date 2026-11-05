package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	bricksconfig "github.com/cristiano-pacheco/bricks/pkg/config"
	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
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
	if cfg.DatabaseHost == "" {
		return sharederrs.ErrDatabaseHostRequired
	}

	if cfg.DatabaseName == "" {
		return sharederrs.ErrDatabaseNameRequired
	}

	if cfg.DatabaseUser == "" {
		return sharederrs.ErrDatabaseUserRequired
	}

	if cfg.DatabasePassword == "" {
		return sharederrs.ErrDatabasePasswordRequired
	}

	port, err := strconv.Atoi(cfg.DatabasePort)
	if err != nil || port < 1 || port > 65535 {
		return sharederrs.ErrDatabasePortInvalid
	}

	if cfg.DatabaseSSLMode == "" {
		cfg.DatabaseSSLMode = "require"
	}

	switch cfg.DatabaseSSLMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return sharederrs.ErrDatabaseSSLModeInvalid
	}

	return nil
}

func validateEndpoints(cfg *Config) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return sharederrs.ErrAPIKeyRequired
	}

	u, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" {
		return sharederrs.ErrPublicBaseURLInvalid
	}

	if cfg.RedisURL == "" {
		return sharederrs.ErrRedisURLRequired
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	if _, _, splitErr := net.SplitHostPort(cfg.HTTPAddr); splitErr != nil {
		return sharederrs.ErrHTTPAddressInvalid
	}

	return nil
}
