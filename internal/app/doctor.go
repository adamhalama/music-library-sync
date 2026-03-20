package app

import (
	"context"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
)

type DoctorUseCase struct {
	Checker *doctor.Checker
}

func (u DoctorUseCase) Run(ctx context.Context, cfg config.Config) doctor.Report {
	checker := u.Checker
	if checker == nil {
		checker = doctor.NewChecker()
	}
	return checker.Check(ctx, cfg)
}
