package challengesolver

type OptionsFunc func(cs *ChallengeSolver)

func WithDryRun(dryrun bool) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.dryRun = dryrun
	}
}
