package garbagecollection

type OptionsFunc func(*GarbageCollectionController)

func WithDryRun(dryrun bool) OptionsFunc {
	return func(gcc *GarbageCollectionController) {
		gcc.dryRun = dryrun
	}
}
