package scdlfreedl

import (
	"fmt"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

// Adapter reserves a separate soundcloud free-download execution path in the
// sync engine. BuildExecSpec is intentionally unsupported because this flow is
// orchestrated track-by-track from the engine.
type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Kind() string {
	return "scdl-freedl"
}

func (a *Adapter) Binary() string {
	return "yt-dlp"
}

func (a *Adapter) MinVersion() string {
	return "2024.1.0"
}

func (a *Adapter) RequiredEnv(source config.Source) []string {
	return nil
}

func (a *Adapter) Validate(source config.Source) error {
	if source.Type != config.SourceTypeSoundCloud {
		return fmt.Errorf("scdl-freedl adapter only supports soundcloud sources")
	}
	return nil
}

func (a *Adapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (engine.ExecSpec, error) {
	return engine.ExecSpec{}, fmt.Errorf("scdl-freedl execution is orchestrated internally by udl")
}
