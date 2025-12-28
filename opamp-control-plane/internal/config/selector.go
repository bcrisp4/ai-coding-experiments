package config

import (
	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// SelectorMatcher matches agents to config selectors.
type SelectorMatcher struct {
	selectors []models.ConfigSelector
}

// NewSelectorMatcher creates a new selector matcher.
func NewSelectorMatcher(selectors []models.ConfigSelector) *SelectorMatcher {
	return &SelectorMatcher{
		selectors: selectors,
	}
}

// Match finds the first matching selector for an agent's labels.
// Returns nil if no selector matches.
func (m *SelectorMatcher) Match(labels map[string]string) *models.ConfigSelector {
	for i := range m.selectors {
		selector := &m.selectors[i]
		if m.matchesSelector(labels, selector) {
			return selector
		}
	}
	return nil
}

// MatchAll returns all selectors that match the agent's labels.
func (m *SelectorMatcher) MatchAll(labels map[string]string) []models.ConfigSelector {
	var matches []models.ConfigSelector
	for _, selector := range m.selectors {
		if m.matchesSelector(labels, &selector) {
			matches = append(matches, selector)
		}
	}
	return matches
}

// matchesSelector checks if labels match a selector's criteria.
func (m *SelectorMatcher) matchesSelector(labels map[string]string, selector *models.ConfigSelector) bool {
	if len(selector.Match.Labels) == 0 {
		return false // Empty match criteria doesn't match anything
	}

	for key, value := range selector.Match.Labels {
		if labels[key] != value {
			return false
		}
	}
	return true
}

// UpdateSelectors replaces the selector list.
func (m *SelectorMatcher) UpdateSelectors(selectors []models.ConfigSelector) {
	m.selectors = selectors
}

// GetSelectors returns the current selector list.
func (m *SelectorMatcher) GetSelectors() []models.ConfigSelector {
	return m.selectors
}
