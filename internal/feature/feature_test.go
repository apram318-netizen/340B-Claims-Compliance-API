package feature

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestParseFeatureEnv_Defaults(t *testing.T) {
	t.Setenv("FEATURE_FLAGS", "")
	r := NewResolver(nil)
	if !r.Global[Webhooks] || !r.Global[ExceptionCases] {
		t.Fatalf("defaults: %+v", r.Global)
	}
	if r.Global[SCIM] {
		t.Fatal("scim should default false")
	}
}

func TestResolver_Enabled_NoDB(t *testing.T) {
	t.Setenv("FEATURE_FLAGS", "webhooks=false")
	r := NewResolver(nil)
	id := uuid.New()
	if r.Enabled(context.Background(), id, Webhooks) {
		t.Fatal("expected false from env")
	}
}
