package fingers

import (
	"fmt"

	"github.com/chainreactors/sdk/pkg/types"
)

func init() {
	// 注册 fingers 引擎到全局注册表
	types.Register("fingers", func(config interface{}) (types.Engine, error) {
		var cfg *Config

		if config == nil {
			cfg = NewConfig() // 使用默认配置
		} else {
			var ok bool
			cfg, ok = config.(*Config)
			if !ok {
				return nil, fmt.Errorf("invalid config type for fingers engine, expected *Config, got %T", config)
			}
		}

		engine, err := NewEngine(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create fingers engine: %w", err)
		}

		return engine, nil
	})
}
