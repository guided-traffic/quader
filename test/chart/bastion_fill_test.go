//go:build chart

package chart_test

import "testing"

// TestR_BAS_054_FillWaitMaxIsAValueOfTheChart (R-BAS-054, R-BAS-046): config.bastion.cache.fill_wait_max renders its
// default of 10m into the role's configuration, a value set is rendered as written, and a value that is no duration is
// refused by the schema with a message naming the key. The range of 30s to 1h is the role's (it refuses a start outside
// it, internal/config); the schema checks the form.
func TestR_BAS_054_FillWaitMaxIsAValueOfTheChart(t *testing.T) {
	t.Run("the default is rendered", func(t *testing.T) {
		_, docs := bstRender(t, bstRequired())
		if got := at(bstConfig(t, docs), "bastion", "cache", "fill_wait_max"); got != "10m" {
			t.Errorf("bastion.cache.fill_wait_max = %v, want 10m", got)
		}
	})
	t.Run("a value set is rendered", func(t *testing.T) {
		_, docs := bstRender(t, bstRequired(), "config.bastion.cache.fill_wait_max=2m")
		if got := at(bstConfig(t, docs), "bastion", "cache", "fill_wait_max"); got != "2m" {
			t.Errorf("bastion.cache.fill_wait_max = %v, want 2m", got)
		}
	})
	t.Run("a value that is no duration is refused", func(t *testing.T) {
		bstFails(t, bstRequired(), []string{"config.bastion.cache.fill_wait_max=ten-minutes"}, "fill_wait_max")
	})
}
