package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Merger combines base configs with overlays.
type Merger struct{}

// NewMerger creates a new config merger.
func NewMerger() *Merger {
	return &Merger{}
}

// Merge combines a base config with an overlay.
// The overlay values take precedence over base values.
func (m *Merger) Merge(base, overlay []byte) ([]byte, error) {
	if len(overlay) == 0 {
		return base, nil
	}
	if len(base) == 0 {
		return overlay, nil
	}

	var baseMap, overlayMap map[string]any

	if err := yaml.Unmarshal(base, &baseMap); err != nil {
		return nil, fmt.Errorf("failed to parse base config: %w", err)
	}

	if err := yaml.Unmarshal(overlay, &overlayMap); err != nil {
		return nil, fmt.Errorf("failed to parse overlay config: %w", err)
	}

	merged := m.deepMerge(baseMap, overlayMap)

	result, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %w", err)
	}

	return result, nil
}

// MergeMultiple merges multiple configs in order.
// Later configs take precedence over earlier ones.
func (m *Merger) MergeMultiple(configs ...[]byte) ([]byte, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	result := configs[0]
	for i := 1; i < len(configs); i++ {
		var err error
		result, err = m.Merge(result, configs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to merge config %d: %w", i, err)
		}
	}

	return result, nil
}

// deepMerge recursively merges two maps.
func (m *Merger) deepMerge(base, overlay map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy base values
	for k, v := range base {
		result[k] = v
	}

	// Apply overlay values
	for k, v := range overlay {
		if baseVal, exists := result[k]; exists {
			// If both values are maps, merge them recursively
			baseMap, baseIsMap := baseVal.(map[string]any)
			overlayMap, overlayIsMap := v.(map[string]any)

			if baseIsMap && overlayIsMap {
				result[k] = m.deepMerge(baseMap, overlayMap)
			} else {
				// Otherwise, overlay wins
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}

	return result
}
