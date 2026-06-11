package types

import zombiecore "github.com/chainreactors/zombie/core"

func NewDefaultZombieOption() *ZombieOption {
	return CloneZombieOption(zombiecore.DefaultRunnerOption)
}

func CloneZombieOption(opt *ZombieOption) *ZombieOption {
	if opt == nil {
		return NewDefaultZombieOption()
	}
	return opt.Clone()
}
