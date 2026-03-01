package services

import (
	"reflect"
	"testing"
)

func TestBuildCycleTrendLabels(t *testing.T) {
	if got := BuildCycleTrendLabels("Cycle %d", 0); len(got) != 0 {
		t.Fatalf("expected empty labels for zero points, got %#v", got)
	}

	defaultLabels := BuildCycleTrendLabels("", 3)
	expectedDefault := []string{"Cycle 1", "Cycle 2", "Cycle 3"}
	if !reflect.DeepEqual(defaultLabels, expectedDefault) {
		t.Fatalf("expected default labels %#v, got %#v", expectedDefault, defaultLabels)
	}

	customLabels := BuildCycleTrendLabels("Цикл %d", 2)
	expectedCustom := []string{"Цикл 1", "Цикл 2"}
	if !reflect.DeepEqual(customLabels, expectedCustom) {
		t.Fatalf("expected custom labels %#v, got %#v", expectedCustom, customLabels)
	}
}
