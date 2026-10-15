package cli

import workflows "github.com/jaa/update-downloads/internal/app"

func tuiDetectOnboardingState(app *AppContext) (tuiOnboardingStartupState, bool) {
	opts := workflows.OnboardingOptions{}
	if app != nil {
		opts.ConfigPath = app.Opts.ConfigPath
		opts.FreeDLConfigPath = app.Opts.FreeDLConfigPath
	}
	return workflows.DetectOnboardingState(opts)
}
