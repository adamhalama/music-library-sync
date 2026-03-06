package scdlfreedl

import (
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

func TestValidateRejectsNonSoundCloud(t *testing.T) {
	adapter := New()
	err := adapter.Validate(config.Source{
		Type: config.SourceTypeSpotify,
	})
	if err == nil {
		t.Fatalf("expected validation error for non-soundcloud source")
	}
}

func TestBuildExecSpecReturnsInternalFlowError(t *testing.T) {
	adapter := New()
	_, err := adapter.BuildExecSpec(config.Source{}, config.Defaults{}, time.Second)
	if err == nil {
		t.Fatalf("expected internal-flow error")
	}
	if !strings.Contains(err.Error(), "orchestrated internally") {
		t.Fatalf("unexpected error: %v", err)
	}
}
