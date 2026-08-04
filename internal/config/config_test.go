package config

import (
	"testing"
)

func TestLoadRequired_PanicsOnMissing(t *testing.T) {
	t.Setenv("HORIZON_URL", "")
	// Clear any existing value
	t.Setenv("NONEXISTENT_VAR_XYZ", "")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for missing required var")
		}
	}()
	loadRequired("NONEXISTENT_VAR_XYZ")
}

func TestLoadInt_UsesDefault(t *testing.T) {
	t.Setenv("FOO_INT_DEFAULT_XYZ", "")
	got := loadInt("FOO_INT_DEFAULT_XYZ", 42)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestLoadInt_ParsesValue(t *testing.T) {
	t.Setenv("FOO_INT_VAL_XYZ", "123")
	got := loadInt("FOO_INT_VAL_XYZ", 0)
	if got != 123 {
		t.Errorf("expected 123, got %d", got)
	}
}

func TestLoadInt_InvalidValue_Panics(t *testing.T) {
	t.Setenv("FOO_INT_BAD_XYZ", "notanumber")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid int")
		}
	}()
	loadInt("FOO_INT_BAD_XYZ", 0)
}

func TestLoadInt64_UsesDefault(t *testing.T) {
	t.Setenv("FOO_INT64_DEFAULT_XYZ", "")
	got := loadInt64("FOO_INT64_DEFAULT_XYZ", 10000)
	if got != 10000 {
		t.Errorf("expected 10000, got %d", got)
	}
}

func TestLoadInt64_ParsesValue(t *testing.T) {
	t.Setenv("FOO_INT64_VAL_XYZ", "5000")
	got := loadInt64("FOO_INT64_VAL_XYZ", 0)
	if got != 5000 {
		t.Errorf("expected 5000, got %d", got)
	}
}
