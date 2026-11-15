package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	KindKustomization = "Kustomization"
	KindHelmRelease   = "HelmRelease"
)

// Resource represents a single target Flux resource.
type Resource struct {
	Kind      string `yaml:"kind" json:"kind"`
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
}

// Key returns a unique identifier for the resource.
func (r Resource) Key() string {
	return fmt.Sprintf("%s/%s/%s", r.Kind, r.Namespace, r.Name)
}

// FluxConfig holds configuration for Flux resources.
type FluxConfig struct {
	Resources []Resource `yaml:"resources" json:"resources"`
}

// Config represents the root configuration file structure.
type Config struct {
	Flux FluxConfig `yaml:"flux" json:"flux"`
}

// Load reads and parses a YAML configuration file from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate checks that the configuration satisfies all rules:
// - at least one resource is configured
// - only known kinds (Kustomization, HelmRelease) are present
// - non-empty namespace and name
// - no duplicate resources
func (c *Config) Validate() error {
	if len(c.Flux.Resources) == 0 {
		return errors.New("flux.resources must contain at least one resource")
	}

	seen := make(map[string]bool)
	for i, res := range c.Flux.Resources {
		if res.Kind != KindKustomization && res.Kind != KindHelmRelease {
			return fmt.Errorf("resource #%d has unsupported kind %q (must be %s or %s)", i+1, res.Kind, KindKustomization, KindHelmRelease)
		}
		if res.Namespace == "" {
			return fmt.Errorf("resource #%d (%s) namespace cannot be empty", i+1, res.Kind)
		}
		if res.Name == "" {
			return fmt.Errorf("resource #%d (%s/%s) name cannot be empty", i+1, res.Kind, res.Namespace)
		}
		key := res.Key()
		if seen[key] {
			return fmt.Errorf("duplicate resource %q found at index #%d", key, i+1)
		}
		seen[key] = true
	}

	return nil
}
