package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// Resolver resolves effective configurations for agents.
type Resolver struct {
	configDir string
	matcher   *SelectorMatcher
	merger    *Merger
	validator Validator
	logger    *slog.Logger

	mu          sync.RWMutex
	baseConfig  []byte
	overlays    map[string][]byte // overlay name -> content
	agentConfigs map[string][]byte // config path -> content
}

// ResolverConfig contains configuration for the resolver.
type ResolverConfig struct {
	ConfigDir    string
	Validator    Validator
	Logger       *slog.Logger
}

// NewResolver creates a new config resolver.
func NewResolver(cfg ResolverConfig) *Resolver {
	return &Resolver{
		configDir:    cfg.ConfigDir,
		matcher:      NewSelectorMatcher(nil),
		merger:       NewMerger(),
		validator:    cfg.Validator,
		logger:       cfg.Logger,
		overlays:     make(map[string][]byte),
		agentConfigs: make(map[string][]byte),
	}
}

// LoadConfigs loads all configurations from the config directory.
func (r *Resolver) LoadConfigs() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Load base config
	basePath := filepath.Join(r.configDir, "base", "collector.yaml")
	if data, err := os.ReadFile(basePath); err == nil {
		r.baseConfig = data
		r.logger.Info("loaded base config", "path", basePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read base config: %w", err)
	}

	// Load overlays
	overlaysDir := filepath.Join(r.configDir, "overlays")
	if entries, err := os.ReadDir(overlaysDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			overlayPath := filepath.Join(overlaysDir, entry.Name(), "collector.yaml")
			if data, err := os.ReadFile(overlayPath); err == nil {
				r.overlays[entry.Name()] = data
				r.logger.Info("loaded overlay", "name", entry.Name(), "path", overlayPath)
			}
		}
	}

	// Load agent configs
	agentsDir := filepath.Join(r.configDir, "agents")
	if err := filepath.WalkDir(agentsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" || filepath.Base(path) == "_selectors.yaml" {
			return nil
		}

		relPath, err := filepath.Rel(agentsDir, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read agent config %s: %w", path, err)
		}

		r.agentConfigs[relPath] = data
		r.logger.Info("loaded agent config", "path", relPath)
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to walk agents directory: %w", err)
	}

	// Load selectors
	selectorsPath := filepath.Join(agentsDir, "_selectors.yaml")
	if data, err := os.ReadFile(selectorsPath); err == nil {
		var sf models.SelectorsFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			return fmt.Errorf("failed to parse selectors file: %w", err)
		}
		r.matcher.UpdateSelectors(sf.Selectors)
		r.logger.Info("loaded selectors", "count", len(sf.Selectors))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read selectors file: %w", err)
	}

	return nil
}

// Resolve returns the effective configuration for an agent.
func (r *Resolver) Resolve(agent *models.Agent) (*models.EffectiveConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Find matching selector
	selector := r.matcher.Match(agent.Labels)
	if selector == nil {
		// No matching selector, use base config if available
		if len(r.baseConfig) == 0 {
			return nil, nil
		}
		hash := r.computeHash(r.baseConfig)
		return &models.EffectiveConfig{
			Name:         "base",
			Hash:         hash,
			Content:      r.baseConfig,
			SelectorName: "",
		}, nil
	}

	// Get the agent-specific config
	agentConfig, ok := r.agentConfigs[selector.Config]
	if !ok {
		return nil, fmt.Errorf("config not found: %s", selector.Config)
	}

	// Start with base config
	configs := [][]byte{}
	if len(r.baseConfig) > 0 {
		configs = append(configs, r.baseConfig)
	}

	// Add overlay if specified
	if selector.Overlay != "" {
		if overlay, ok := r.overlays[selector.Overlay]; ok {
			configs = append(configs, overlay)
		} else {
			r.logger.Warn("overlay not found", "overlay", selector.Overlay, "selector", selector.Name)
		}
	}

	// Add agent-specific config
	configs = append(configs, agentConfig)

	// Merge all configs
	merged, err := r.merger.MergeMultiple(configs...)
	if err != nil {
		return nil, fmt.Errorf("failed to merge configs: %w", err)
	}

	// Validate merged config
	if r.validator != nil {
		if err := r.validator.ValidateYAML(merged); err != nil {
			return nil, fmt.Errorf("merged config validation failed: %w", err)
		}
		if err := r.validator.ValidateOTelConfig(merged); err != nil {
			return nil, fmt.Errorf("merged config OTel validation failed: %w", err)
		}
	}

	hash := r.computeHash(merged)
	return &models.EffectiveConfig{
		Name:         selector.Name,
		Hash:         hash,
		Content:      merged,
		SelectorName: selector.Name,
	}, nil
}

// GetConfigForSelector returns the resolved config for a specific selector.
func (r *Resolver) GetConfigForSelector(selectorName string) (*models.EffectiveConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var selector *models.ConfigSelector
	for _, s := range r.matcher.GetSelectors() {
		if s.Name == selectorName {
			selector = &s
			break
		}
	}

	if selector == nil {
		return nil, fmt.Errorf("selector not found: %s", selectorName)
	}

	// Get the agent-specific config
	agentConfig, ok := r.agentConfigs[selector.Config]
	if !ok {
		return nil, fmt.Errorf("config not found: %s", selector.Config)
	}

	configs := [][]byte{}
	if len(r.baseConfig) > 0 {
		configs = append(configs, r.baseConfig)
	}

	if selector.Overlay != "" {
		if overlay, ok := r.overlays[selector.Overlay]; ok {
			configs = append(configs, overlay)
		}
	}

	configs = append(configs, agentConfig)

	merged, err := r.merger.MergeMultiple(configs...)
	if err != nil {
		return nil, err
	}

	hash := r.computeHash(merged)
	return &models.EffectiveConfig{
		Name:         selector.Name,
		Hash:         hash,
		Content:      merged,
		SelectorName: selector.Name,
	}, nil
}

// GetSelectors returns the current list of selectors.
func (r *Resolver) GetSelectors() []models.ConfigSelector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.matcher.GetSelectors()
}

// computeHash calculates the SHA256 hash of content.
func (r *Resolver) computeHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// ConfigChangeCallback is called when configs change.
type ConfigChangeCallback func(changedSelectors []string)

// SetConfigDir updates the config directory and reloads.
func (r *Resolver) SetConfigDir(dir string) error {
	r.mu.Lock()
	r.configDir = dir
	r.mu.Unlock()
	return r.LoadConfigs()
}
