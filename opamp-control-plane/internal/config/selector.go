package config

import (
	"sync"

	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// SelectorMatcher matches agents to config selectors.
// It is safe for concurrent use.
type SelectorMatcher struct {
	selectors []models.ConfigSelector
	mu        sync.RWMutex
}

// NewSelectorMatcher creates a new selector matcher.
func NewSelectorMatcher(selectors []models.ConfigSelector) *SelectorMatcher {
	return &SelectorMatcher{
		selectors: selectors,
	}
}

// Match finds the first matching selector for an agent's labels.
// Returns nil if no selector matches.
// The returned selector is a copy, safe to use after the lock is released.
func (m *SelectorMatcher) Match(labels map[string]string) *models.ConfigSelector {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.selectors {
		if m.matchesSelector(labels, &m.selectors[i]) {
			// Return a copy to avoid data race with UpdateSelectors
			result := m.selectors[i]
			return &result
		}
	}
	return nil
}

// MatchAll returns all selectors that match the agent's labels.
func (m *SelectorMatcher) MatchAll(labels map[string]string) []models.ConfigSelector {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []models.ConfigSelector
	for _, selector := range m.selectors {
		if m.matchesSelector(labels, &selector) {
			matches = append(matches, selector)
		}
	}
	return matches
}

// matchesSelector checks if labels match a selector's criteria.
// Caller must hold at least a read lock.
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectors = selectors
}

// GetSelectors returns a copy of the current selector list.
func (m *SelectorMatcher) GetSelectors() []models.ConfigSelector {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]models.ConfigSelector, len(m.selectors))
	copy(result, m.selectors)
	return result
}
