package controller

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Providers []*Provider `json:"providers"`
}

func (c *Controller) readConfig(cfg *Config, param *ParamRun) error {
	if param.ConfigBody != "" {
		if err := decodeConfig(strings.NewReader(param.ConfigBody), cfg); err != nil {
			return fmt.Errorf("marshal configuration as YAML: %w", err)
		}
		return nil
	}
	if param.ConfigFilePath == "" {
		// Don't search a configuration file from the current directory to the root directory to prevent being replaced with a fake configuration file
		param.ConfigFilePath = ".tfprovidercheck.yaml"
	}
	return c.readConfigFile(cfg, param.ConfigFilePath)
}

func (c *Controller) readConfigFile(cfg *Config, cfgFilePath string) error {
	f, err := c.fs.Open(cfgFilePath)
	if err != nil {
		return fmt.Errorf("open a configuration file: %w", err)
	}
	defer f.Close()
	if err := decodeConfig(f, cfg); err != nil {
		return fmt.Errorf("read a configuration file as YAML: %w", err)
	}
	return nil
}

// decodeConfig decodes a configuration as YAML strictly.
// Unknown fields such as typos (e.g. `versoin`) and invalid scalar types are rejected
// so that a misconfigured entry never silently degrades into a permissive one.
func decodeConfig(r io.Reader, cfg *Config) error {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return err //nolint:wrapcheck
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrMultipleConfigDocuments
		}
		return err //nolint:wrapcheck
	}
	return nil
}
