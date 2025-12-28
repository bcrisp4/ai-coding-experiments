// Package config provides configuration resolution and validation.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Validator validates OTel Collector configurations.
type Validator interface {
	// ValidateYAML checks YAML syntax.
	ValidateYAML(content []byte) error

	// ValidateOTelConfig checks OTel Collector schema.
	ValidateOTelConfig(content []byte) error
}

// DefaultValidator implements basic YAML and OTel config validation.
type DefaultValidator struct {
	StrictSchema bool
}

// NewValidator creates a new config validator.
func NewValidator(strictSchema bool) *DefaultValidator {
	return &DefaultValidator{
		StrictSchema: strictSchema,
	}
}

// ValidateYAML checks if the content is valid YAML.
func (v *DefaultValidator) ValidateYAML(content []byte) error {
	var data any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	return nil
}

// OTelConfig represents the basic structure of an OTel Collector config.
type OTelConfig struct {
	Receivers  map[string]any `yaml:"receivers"`
	Processors map[string]any `yaml:"processors"`
	Exporters  map[string]any `yaml:"exporters"`
	Extensions map[string]any `yaml:"extensions"`
	Service    *ServiceConfig `yaml:"service"`
}

// ServiceConfig represents the service section of an OTel Collector config.
type ServiceConfig struct {
	Extensions []string              `yaml:"extensions"`
	Pipelines  map[string]*Pipeline  `yaml:"pipelines"`
	Telemetry  map[string]any        `yaml:"telemetry"`
}

// Pipeline represents a telemetry pipeline.
type Pipeline struct {
	Receivers  []string `yaml:"receivers"`
	Processors []string `yaml:"processors"`
	Exporters  []string `yaml:"exporters"`
}

// ValidateOTelConfig validates the OTel Collector configuration structure.
func (v *DefaultValidator) ValidateOTelConfig(content []byte) error {
	var config OTelConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return fmt.Errorf("failed to parse OTel config: %w", err)
	}

	// Validate service section exists
	if config.Service == nil {
		return fmt.Errorf("missing required 'service' section")
	}

	// Validate pipelines exist
	if config.Service.Pipelines == nil || len(config.Service.Pipelines) == 0 {
		return fmt.Errorf("no pipelines defined in service section")
	}

	// Validate each pipeline references existing components
	for name, pipeline := range config.Service.Pipelines {
		if pipeline == nil {
			continue
		}

		// Validate pipeline has at least one receiver and exporter
		if len(pipeline.Receivers) == 0 {
			return fmt.Errorf("pipeline %q has no receivers", name)
		}
		if len(pipeline.Exporters) == 0 {
			return fmt.Errorf("pipeline %q has no exporters", name)
		}

		// If strict mode, validate that referenced components exist
		if v.StrictSchema {
			for _, receiver := range pipeline.Receivers {
				if _, ok := config.Receivers[receiver]; !ok {
					return fmt.Errorf("pipeline %q references undefined receiver %q", name, receiver)
				}
			}

			for _, processor := range pipeline.Processors {
				if _, ok := config.Processors[processor]; !ok {
					return fmt.Errorf("pipeline %q references undefined processor %q", name, processor)
				}
			}

			for _, exporter := range pipeline.Exporters {
				if _, ok := config.Exporters[exporter]; !ok {
					return fmt.Errorf("pipeline %q references undefined exporter %q", name, exporter)
				}
			}
		}

		// Validate extensions if referenced
		if v.StrictSchema {
			for _, ext := range config.Service.Extensions {
				if _, ok := config.Extensions[ext]; !ok {
					return fmt.Errorf("service references undefined extension %q", ext)
				}
			}
		}
	}

	return nil
}

// ValidationResult contains the result of config validation.
type ValidationResult struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

// ValidateConfig performs full validation and returns a detailed result.
func (v *DefaultValidator) ValidateConfig(content []byte) ValidationResult {
	result := ValidationResult{Valid: true}

	// Check YAML syntax
	if err := v.ValidateYAML(content); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	// Check OTel schema
	if err := v.ValidateOTelConfig(content); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	return result
}
