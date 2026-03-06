package app

import "github.com/jaa/update-downloads/internal/config"

type ValidateUseCase struct{}

func (ValidateUseCase) Run(cfg config.Config) error {
	return config.Validate(cfg)
}
