package gogo

import (
	"fmt"

	"github.com/chainreactors/sdk/pkg/types"
)

func init() {
	// 注册 gogo 引擎到全局注册表
	types.Register("gogo", func(config interface{}) (types.Engine, error) {
		var cfg *Config

		if config == nil {
			cfg = nil // NewGogoEngine 会使用默认配置
		} else {
			var ok bool
			cfg, ok = config.(*Config)
			if !ok {
				return nil, fmt.Errorf("invalid config type for gogo engine, expected *gogo.Config, got %T", config)
			}
		}

		engine := NewGogoEngine(cfg)
		if err := engine.Init(); err != nil {
			return nil, fmt.Errorf("failed to initialize gogo engine: %w", err)
		}

		return engine, nil
	})
}
