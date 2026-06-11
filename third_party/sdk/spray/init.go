package spray

import (
	"fmt"

	"github.com/chainreactors/sdk/pkg/types"
)

func init() {
	// 注册 spray 引擎到全局注册表
	types.Register("spray", func(config interface{}) (types.Engine, error) {
		var cfg *Config

		if config == nil {
			cfg = nil // NewSprayEngine 会使用默认配置
		} else {
			var ok bool
			cfg, ok = config.(*Config)
			if !ok {
				return nil, fmt.Errorf("invalid config type for spray engine, expected *spray.Config, got %T", config)
			}
		}

		engine := NewSprayEngine(cfg)
		if err := engine.Init(); err != nil {
			return nil, fmt.Errorf("failed to initialize spray engine: %w", err)
		}

		return engine, nil
	})
}
