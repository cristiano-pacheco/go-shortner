package config

import (
	"errors"
	"net/url"
	"strings"

	bricksconfig "github.com/cristiano-pacheco/bricks/pkg/config"
)

type Config struct {
	APIKey        string `config:"api_key"`
	PublicBaseURL string `config:"public_base_url"`
}

func Load() (Config, error) {
	loaded, err := bricksconfig.New[Config](bricksconfig.WithPath("app"))
	if err != nil {
		return Config{}, err
	}

	cfg := loaded.Get()
	if err = validateEndpoints(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
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

	return nil
}
