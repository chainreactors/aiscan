package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	gkcfg "github.com/gookit/config/v2"
	yamldrv "github.com/gookit/config/v2/yaml"
)

const DefaultConfigName = "config.yaml"

func init() {
	gkcfg.WithOptions(func(opt *gkcfg.Options) {
		opt.DecoderConfig.TagName = "config"
		opt.ParseDefault = true
	})
	gkcfg.AddDriver(yamldrv.Driver)
}

func newConfigLoader() *gkcfg.Config {
	c := gkcfg.New("aiscan")
	c.WithOptions(func(opt *gkcfg.Options) {
		opt.DecoderConfig.TagName = "config"
	})
	c.AddDriver(yamldrv.Driver)
	return c
}

func LoadConfig(filename string, v interface{}) error {
	c := newConfigLoader()
	if err := c.LoadFiles(filename); err != nil {
		return err
	}
	if err := c.Decode(v); err != nil {
		return err
	}
	applyExplicitReconNumericOptions(c, v)
	return nil
}

func applyExplicitReconNumericOptions(c *gkcfg.Config, v interface{}) {
	opt, ok := v.(*Option)
	if !ok || opt == nil {
		return
	}
	if c.Exists("recon.limit") {
		v := c.Int("recon.limit")
		opt.ReconLimit = &v
	}
}

func findDefaultConfigFile() string {
	if _, err := os.Stat(DefaultConfigName); err == nil {
		return DefaultConfigName
	}
	if dir, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(dir, "aiscan", DefaultConfigName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func LoadAndApplyConfig(option *Option) (string, error) {
	configPath := option.ConfigFile
	if configPath == "" {
		configPath = findDefaultConfigFile()
	}
	if configPath == "" {
		return "", nil
	}
	if _, err := os.Stat(configPath); err != nil {
		if option.ConfigFile == "" && os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("config file %s: %w", configPath, err)
	}

	var loaded Option
	if err := LoadConfig(configPath, &loaded); err != nil {
		return configPath, fmt.Errorf("load config %s: %w", configPath, err)
	}
	mergeOption(option, &loaded)
	if err := loadRuntimeDefaults(configPath); err != nil {
		return configPath, fmt.Errorf("load runtime defaults %s: %w", configPath, err)
	}
	return configPath, nil
}

func loadRuntimeDefaults(filename string) error {
	c := newConfigLoader()
	if err := c.LoadFiles(filename); err != nil {
		return err
	}
	if v := c.String("scan.verify"); v != "" {
		DefaultVerify = v
	}
	if v := c.Int("scan.verify_timeout"); v > 0 {
		DefaultVerifyTimeout = strconv.Itoa(v)
	}
	if v := c.String("search.tavily_keys"); v != "" {
		DefaultTavilyKeys = v
	}
	if v := c.String("websearch.proxy"); v != "" {
		DefaultSearchProxy = v
	}
	return nil
}

func mergeOption(dst, src *Option) {
	dst.Provider = ResolveString(dst.Provider, src.Provider)
	dst.BaseURL = ResolveString(dst.BaseURL, src.BaseURL)
	dst.APIKey = ResolveString(dst.APIKey, src.APIKey)
	dst.Model = ResolveString(dst.Model, src.Model)
	dst.LLMProxy = ResolveString(dst.LLMProxy, src.LLMProxy)
	dst.CyberhubURL = ResolveString(dst.CyberhubURL, src.CyberhubURL)
	dst.CyberhubKey = ResolveString(dst.CyberhubKey, src.CyberhubKey)
	dst.CyberhubMode = ResolveString(dst.CyberhubMode, src.CyberhubMode)
	if !dst.CyberhubDraft {
		dst.CyberhubDraft = src.CyberhubDraft
	}
	dst.FofaEmail = ResolveString(dst.FofaEmail, src.FofaEmail)
	dst.FofaKey = ResolveString(dst.FofaKey, src.FofaKey)
	dst.HunterToken = ResolveString(dst.HunterToken, src.HunterToken)
	dst.HunterAPIKey = ResolveString(dst.HunterAPIKey, src.HunterAPIKey)
	dst.ReconProxy = ResolveString(dst.ReconProxy, src.ReconProxy)
	if dst.ReconLimit == nil && src.ReconLimit != nil {
		dst.ReconLimit = src.ReconLimit
	}
	dst.Proxy = ResolveString(dst.Proxy, src.Proxy)
	dst.IOAURL = ResolveString(dst.IOAURL, src.IOAURL)
	dst.IOANodeName = ResolveString(dst.IOANodeName, src.IOANodeName)
	if (dst.Space == "" || dst.Space == "default") && src.Space != "" {
		dst.Space = src.Space
	}
}

func InitDefaultConfig() string {
	return defaultConfigTemplate
}

const defaultConfigTemplate = `# aiscan 配置文件
#
# 编译时: build.sh 读取此文件，通过 ldflags 将配置固化到二进制
# 运行时: aiscan 自动加载 ./config.yaml 或 ~/.config/aiscan/config.yaml
# 优先级: -c 自定义配置 > CLI 参数 > 默认 config.yaml > 编译时固化值
#
# 仅填写需要的字段，留空或删除的字段不会覆盖其他来源的值

# LLM Provider 配置
llm:
  # openai, deepseek, openrouter, ollama, groq, moonshot, anthropic
  provider: ""
  # API base URL（留空使用 provider 默认值）
  base_url: ""
  # API key（建议使用环境变量而非写入文件）
  api_key: ""
  # 模型名称
  model: ""
  # LLM API 代理
  proxy: ""

# Cyberhub 资源服务
cyberhub:
  url: ""
  key: ""
  # merge 或 override
  mode: ""
  # 扫描器代理，支持以下格式:
  #   socks5://127.0.0.1:1080
  #   trojan://password@server:443?sni=example.com
  #   clash://?url=<encoded-subscribe-url>&strategy=adaptive
  proxy: ""

# 搜索
search:
  # Tavily API keys（逗号分隔，留空则 fallback 到 DuckDuckGo）
  tavily_keys: ""

# Web 搜索
websearch:
  # web_search 请求代理（如 http://127.0.0.1:7890 或 socks5://...）
  proxy: ""

# IOA 协作
ioa:
  url: ""
  db: ""
  node_name: ""
  space: ""

# 资产测绘 (通过 uncover SDK)
# FOFA 凭证从此处或环境变量 FOFA_EMAIL / FOFA_KEY 读取
# 额外 source (Shodan/Censys/...) 通过环境变量或 ~/.uncover-config/provider-config.yaml 配置
recon:
  fofa_email: ""
  fofa_key: ""
  hunter_token: ""    # 极少用; Hunter web 端 token
  hunter_api_key: ""  # 华顺信安后台 API 管理生成的 64 位 hex key
  proxy: ""           # 出站代理 (Hunter 屏蔽境外 IP, 中国 VPS 走 socks5://host:1080)
  limit: 0            # 单次查询最多返回多少 asset, 0 = 不限

# 扫描默认值
scan:
  # auto, off, low, medium, high, critical
  verify: ""
  # 单次验证超时秒数（0 表示不覆盖）
  verify_timeout: 0

# 通用选项
misc:
  debug: false
  quiet: false
  no_color: false

# 以下仅 build.sh 使用
build:
  osarch: ""
  tags: ""
  output: dist
`
