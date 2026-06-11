package spray

import "github.com/chainreactors/sdk/pkg/types"

type Option = types.SprayOption

func NewDefaultOption() *Option {
	return types.NewDefaultSprayOption()
}

func cloneOption(opt *Option) *Option {
	return types.CloneSprayOption(opt)
}
