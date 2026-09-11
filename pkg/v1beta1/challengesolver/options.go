package challengesolver

type OptionsFunc func(cs *ChallengeSolver)

func WithDryRun(dryrun bool) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.dryRun = dryrun
	}
}

// WithPlugins replaces the coordinator's plugin list with the provided plugins.
// The first plugin whose Applicable returns true handles each challenge.
func WithPlugins(plugins ...ChallengePlugin) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.plugins = plugins
	}
}
