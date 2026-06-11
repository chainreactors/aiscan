package neutron

import (
	"fmt"

	"github.com/chainreactors/sdk/pkg/types"
)

func init() {
	// 注册 neutron 引擎到全局注册表
	types.Register("neutron", func(config interface{}) (types.Engine, error) {
		var cfg *Config

		if config == nil {
			cfg = NewConfig()
		} else {
			var ok bool
			cfg, ok = config.(*Config)
			if !ok {
				return nil, fmt.Errorf("invalid config type for neutron engine, expected *neutron.Config, got %T", config)
			}
		}

		engine, err := NewEngine(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create neutron engine: %w", err)
		}

		return engine, nil
	})
}
