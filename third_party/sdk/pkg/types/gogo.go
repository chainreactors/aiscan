package types

import gogopkg "github.com/chainreactors/gogo/v2/pkg"

func NewDefaultGogoOption() *GogoOption {
	return CloneGogoOption(gogopkg.DefaultRunnerOption)
}

func CloneGogoOption(opt *GogoOption) *GogoOption {
	if opt == nil {
		opt = gogopkg.DefaultRunnerOption
	}
	clone := *opt
	if opt.ScanFilters != nil {
		clone.ScanFilters = make([][]string, len(opt.ScanFilters))
		for i, filter := range opt.ScanFilters {
			clone.ScanFilters[i] = append([]string(nil), filter...)
		}
	}
	if opt.ExcludeCIDRs != nil {
		clone.ExcludeCIDRs = append(clone.ExcludeCIDRs[:0:0], opt.ExcludeCIDRs...)
	}
	return &clone
}
