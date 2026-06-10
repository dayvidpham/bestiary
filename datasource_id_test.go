package bestiary_test

import "testing"

import "github.com/dayvidpham/bestiary"

// TestDataSourceID_Constants locks the DataSourceID zero value and the two named
// source constants so downstream consumers can rely on exact string values.
func TestDataSourceID_Constants(t *testing.T) {
	if bestiary.DataSourceNone != "" {
		t.Errorf("DataSourceNone = %q, want \"\" (zero value)", bestiary.DataSourceNone)
	}
	if bestiary.DataSourceModelsDev != "models.dev" {
		t.Errorf("DataSourceModelsDev = %q, want \"models.dev\"", bestiary.DataSourceModelsDev)
	}
	if bestiary.DataSourceOllama != "ollama" {
		t.Errorf("DataSourceOllama = %q, want \"ollama\"", bestiary.DataSourceOllama)
	}
}
