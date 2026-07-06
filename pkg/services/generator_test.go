package services

import "testing"

func TestDefaultGeneratorAdapterIsHugo(t *testing.T) {
	adapter := CurrentGeneratorAdapter()
	if adapter == nil {
		t.Fatal("CurrentGeneratorAdapter() returned nil")
	}
	if adapter.Name() != "hugo" {
		t.Fatalf("adapter name = %q, want hugo", adapter.Name())
	}
}

func TestSetGeneratorAdapterRejectsNil(t *testing.T) {
	if err := SetGeneratorAdapter(nil); err == nil {
		t.Fatal("SetGeneratorAdapter(nil) should fail")
	}
}
