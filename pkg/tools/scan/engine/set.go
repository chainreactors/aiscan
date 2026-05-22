package engine

import (
	"context"

	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	anigo "github.com/chainreactors/ani-go"
	_ "github.com/chainreactors/ani-go/aqc"        // register aqc_unauth + aqc (authed) sources
	_ "github.com/chainreactors/ani-go/qcc"        // register qcc source (needs QCCSESSID cookie)
	_ "github.com/chainreactors/ani-go/tyc"        // register tyc source (needs auth_token JWT)
	_ "github.com/chainreactors/ani-go/tyc_unauth" // register tyc_unauth source
	inago "github.com/chainreactors/ina-go"
	_ "github.com/chainreactors/ina-go/fofa"   // register fofa source
	_ "github.com/chainreactors/ina-go/hunter" // register hunter source
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/association"
	"github.com/chainreactors/sdk/spray"
	sdkzombie "github.com/chainreactors/sdk/zombie"
)

// ReconOptions 提供 ina-go / ani-go 资产测绘引擎所需的凭证与默认行为。
// 任一 source 凭证非空就会让 ina engine 初始化对应 client; aqc_unauth/tyc_unauth 不需要凭证。
type ReconOptions struct {
	FofaEmail     string
	FofaKey       string
	HunterToken   string // 极少用 — 抓包出来的 web 登录 cookie/JWT, Python 原版 token 模式
	HunterAPIKey  string // 华顺信安后台 API 管理生成的 api-key (推荐, 64 位 hex)
	Limit         int
	IngressProxy  string // 给 ina-go 的全局出站代理 (http://, https://, socks5://, socks5h://)
	AniDepth      int    // 投资链路递归深度, 默认 1
	AniDepthSet   bool
	AniPercent    float64 // 子公司入选最小持股比例, 默认 0.5
	AniPercentSet bool
	AniProxy      string
	AniTycToken   string // 天眼查 auth_token JWT (Phase 2, tyc 源)
	AniQccCookie  string // 企查查 QCCSESSID (Phase 2, qcc 源)
	AniAqcCookie  string // 爱企查 BAIDUID (Phase 2, aqc authed 源)
}

const (
	DefaultAniDepth   = 1
	DefaultAniPercent = 0.5
)

type Set struct {
	Fingers   *sdkfingers.Engine
	Gogo      *gogo.GogoEngine
	Spray     *spray.SprayEngine
	Neutron   *neutron.Engine
	Zombie    *sdkzombie.Engine
	Ina       *inago.Engine
	Ani       *anigo.Engine
	Index     *association.FingerPOCIndex
	Resources *resources.Set
	Capacity  CapacityConfig
	Recon     ReconOptions
}

// CapacityConfig holds per-engine capacity limits. Zero means unlimited.
type CapacityConfig struct {
	Gogo    int // total concurrent scan threads (default: 5000)
	Spray   int // total concurrent HTTP threads (default: 200)
	Zombie  int // total concurrent auth threads (default: 500)
	Neutron int // total concurrent template executions (default: 10)
}

// DefaultCapacity returns sensible capacity defaults.
func DefaultCapacity() CapacityConfig {
	return CapacityConfig{
		Gogo:    800,
		Spray:   100,
		Zombie:  100,
		Neutron: 100,
	}
}

func (e *Set) Close() {
	if e.Fingers != nil {
		e.Fingers.Close()
	}
	if e.Gogo != nil {
		e.Gogo.Close()
	}
	if e.Spray != nil {
		e.Spray.Close()
	}
	if e.Neutron != nil {
		e.Neutron.Close()
	}
	if e.Zombie != nil {
		e.Zombie.Close()
	}
	if e.Ina != nil {
		_ = e.Ina.Close()
	}
	if e.Ani != nil {
		_ = e.Ani.Close()
	}
}

func Init(ctx context.Context, cyberhubURL, apiKey string) (*Set, error) {
	return InitWithLogger(ctx, cyberhubURL, apiKey, telemetry.NopLogger())
}

func InitWithLogger(ctx context.Context, cyberhubURL, apiKey string, logger telemetry.Logger) (*Set, error) {
	return InitWithOptions(ctx, resources.Options{
		CyberhubURL: cyberhubURL,
		APIKey:      apiKey,
		Mode:        resources.ModeMerge,
	}, logger)
}

func InitWithOptions(ctx context.Context, opts resources.Options, logger telemetry.Logger) (*Set, error) {
	return InitWithCapacity(ctx, opts, DefaultCapacity(), logger)
}

func InitWithCapacity(ctx context.Context, opts resources.Options, caps CapacityConfig, logger telemetry.Logger) (*Set, error) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	set := &Set{}

	resourceSet, err := resources.Init(ctx, opts)
	if err != nil {
		return nil, err
	}
	set.Resources = resourceSet
	if resourceSet.RemoteEnabled {
		logger.Infof("resources source=cyberhub mode=%s fingers=%d neutron=%d", resourceSet.Mode, resourceSet.RemoteFingers, resourceSet.RemoteNeutron)
		if resourceSet.RemoteFingersErr != nil {
			logger.Warnf("resources source=cyberhub type=fingers error=%q fallback=local", resourceSet.RemoteFingersErr)
		} else if resourceSet.RemoteFingers == 0 {
			logger.Warnf("resources source=cyberhub type=fingers count=0 fallback=local")
		}
		if resourceSet.RemoteNeutronErr != nil {
			logger.Warnf("resources source=cyberhub type=neutron error=%q fallback=local", resourceSet.RemoteNeutronErr)
		} else if resourceSet.RemoteNeutron == 0 {
			logger.Warnf("resources source=cyberhub type=neutron count=0 fallback=local")
		}
	}

	fEngine := resourceSet.Fingers
	if fEngine == nil {
		logger.Warnf("engine=fingers templates=0 action=disable")
	} else if fEngine.Count() > 0 {
		set.Fingers = fEngine
		logger.Infof("engine=fingers status=ready templates=%d", fEngine.Count())
	} else {
		logger.Warnf("engine=fingers templates=0 action=disable")
		_ = fEngine.Close()
	}

	nEngine := resourceSet.Neutron
	if nEngine != nil && nEngine.Count() > 0 {
		set.Neutron = nEngine
		logger.Infof("engine=neutron status=ready templates=%d", nEngine.Count())
	} else {
		logger.Warnf("engine=neutron templates=0 action=disable")
		if nEngine != nil {
			_ = nEngine.Close()
		}
	}

	if set.Neutron != nil {
		set.Index = association.NewFingerPOCIndex()
		set.Index.BuildFromTemplates(set.Neutron.Get())
		fingerCount, pocCount := set.Index.Count()
		logger.Infof("index=finger_poc status=ready fingers=%d pocs=%d", fingerCount, pocCount)
	}

	gogoConfig := gogo.NewConfig()
	gogoConfig.WithResourceProvider(resourceSet.GogoConfig)
	if set.Fingers != nil {
		gogoConfig.WithFingersEngine(set.Fingers)
	}
	if set.Neutron != nil {
		gogoConfig.WithNeutronEngine(set.Neutron)
	}
	if caps.Gogo > 0 {
		gogoConfig.WithCapacity(caps.Gogo)
	}
	set.Gogo = gogo.NewEngine(gogoConfig)
	logger.Infof("engine=gogo status=ready")

	sprayConfig := spray.NewConfig()
	sprayConfig.WithResourceProvider(resourceSet.SprayConfig)
	if set.Fingers != nil {
		sprayConfig.WithFingersEngine(set.Fingers)
	}
	if caps.Spray > 0 {
		sprayConfig.WithCapacity(caps.Spray)
	}
	set.Spray = spray.NewEngine(sprayConfig)
	logger.Infof("engine=spray status=ready")

	zombieConfig := sdkzombie.NewConfig()
	zombieConfig.WithResourceProvider(resourceSet.ZombieConfig)
	if caps.Zombie > 0 {
		zombieConfig.WithCapacity(caps.Zombie)
	}
	set.Zombie = sdkzombie.NewEngine(zombieConfig)
	if err := set.Zombie.Init(); err != nil {
		logger.Warnf("engine=zombie status=disabled error=%q", err)
		set.Zombie = nil
	} else {
		logger.Infof("engine=zombie status=ready")
	}

	if set.Neutron != nil && caps.Neutron > 0 {
		set.Neutron.SetCapacity(caps.Neutron)
	}

	set.Capacity = caps
	return set, nil
}

// SetupIna 在 InitWithOptions 之后追加 ina-go engine 初始化, 把 ReconOptions
// 的凭证注入。任一 source 凭证非空就 init; 都为空时 Ina 留 nil 让 ina 工具不注册。
// 可被反复调用: opts 的非零字段累加进 e.Recon, engine 基于合并后的全集重建。
func (e *Set) SetupIna(opts ReconOptions, logger telemetry.Logger) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	e.Recon = mergeReconOptions(e.Recon, opts)
	eff := e.Recon
	fofaOK := eff.FofaEmail != "" && eff.FofaKey != ""
	hunterOK := eff.HunterToken != "" || eff.HunterAPIKey != ""
	if !fofaOK && !hunterOK {
		return
	}
	cfg := inago.NewConfig().
		WithLimit(eff.Limit).
		WithLogger(&inaLoggerAdapter{logger: logger})
	if eff.IngressProxy != "" {
		cfg.WithProxy(eff.IngressProxy)
	}
	if fofaOK {
		cfg.WithFofa(eff.FofaEmail, eff.FofaKey)
	}
	if eff.HunterToken != "" {
		cfg.WithHunterToken(eff.HunterToken)
	}
	if eff.HunterAPIKey != "" {
		cfg.WithHunterAPIKey(eff.HunterAPIKey)
	}
	if e.Ina != nil {
		_ = e.Ina.Close()
	}
	e.Ina = inago.NewEngine(cfg)
	logger.Infof("engine=ina status=ready sources=%v", e.Ina.Sources())
}

// SetupAni 总是初始化 ani-go engine (aqc_unauth 不需要凭证)。
// 与 SetupIna 一致: opts 的显式字段并入 e.Recon, engine 基于合并后的全集重建;
// 未提供的 depth/percent 退回默认, 但**不**污染 e.Recon, 这样后续调用仍能
// 看到"这两个字段从未被显式设置过"。
func (e *Set) SetupAni(opts ReconOptions, logger telemetry.Logger) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	e.Recon = mergeReconOptions(e.Recon, opts)
	eff := normalizeAniOptions(e.Recon)
	cfg := anigo.NewConfig().WithLogger(&aniLoggerAdapter{logger: logger})
	cfg.WithDepth(eff.AniDepth)
	cfg.WithPercent(eff.AniPercent)
	if eff.AniProxy != "" {
		cfg.WithProxy(eff.AniProxy)
	}
	if eff.AniTycToken != "" {
		cfg.WithTycToken(eff.AniTycToken)
	}
	if eff.AniQccCookie != "" {
		cfg.WithQccCookie(eff.AniQccCookie)
	}
	if eff.AniAqcCookie != "" {
		cfg.WithAqcCookie(eff.AniAqcCookie)
	}
	if e.Ani != nil {
		_ = e.Ani.Close()
	}
	e.Ani = anigo.NewEngine(cfg)
	logger.Infof("engine=ani status=ready sources=%v", e.Ani.Sources())
}

func normalizeAniOptions(opts ReconOptions) ReconOptions {
	if !opts.AniDepthSet {
		opts.AniDepth = DefaultAniDepth
		opts.AniDepthSet = true
	}
	if !opts.AniPercentSet {
		opts.AniPercent = DefaultAniPercent
		opts.AniPercentSet = true
	}
	return opts
}

func mergeReconOptions(base, next ReconOptions) ReconOptions {
	if next.FofaEmail != "" {
		base.FofaEmail = next.FofaEmail
	}
	if next.FofaKey != "" {
		base.FofaKey = next.FofaKey
	}
	if next.HunterToken != "" {
		base.HunterToken = next.HunterToken
	}
	if next.HunterAPIKey != "" {
		base.HunterAPIKey = next.HunterAPIKey
	}
	if next.IngressProxy != "" {
		base.IngressProxy = next.IngressProxy
	}
	if next.Limit != 0 {
		base.Limit = next.Limit
	}
	if next.AniDepthSet {
		base.AniDepth = next.AniDepth
		base.AniDepthSet = true
	}
	if next.AniPercentSet {
		base.AniPercent = next.AniPercent
		base.AniPercentSet = true
	}
	if next.AniProxy != "" {
		base.AniProxy = next.AniProxy
	}
	if next.AniTycToken != "" {
		base.AniTycToken = next.AniTycToken
	}
	if next.AniQccCookie != "" {
		base.AniQccCookie = next.AniQccCookie
	}
	if next.AniAqcCookie != "" {
		base.AniAqcCookie = next.AniAqcCookie
	}
	return base
}

// inaLoggerAdapter / aniLoggerAdapter 把 telemetry.Logger 适配到 SDK 的 Logger 接口。
type inaLoggerAdapter struct{ logger telemetry.Logger }

func (a *inaLoggerAdapter) Debugf(format string, args ...any) { a.logger.Debugf(format, args...) }
func (a *inaLoggerAdapter) Infof(format string, args ...any)  { a.logger.Infof(format, args...) }
func (a *inaLoggerAdapter) Warnf(format string, args ...any)  { a.logger.Warnf(format, args...) }
func (a *inaLoggerAdapter) Errorf(format string, args ...any) { a.logger.Errorf(format, args...) }

type aniLoggerAdapter struct{ logger telemetry.Logger }

func (a *aniLoggerAdapter) Debugf(format string, args ...any) { a.logger.Debugf(format, args...) }
func (a *aniLoggerAdapter) Infof(format string, args ...any)  { a.logger.Infof(format, args...) }
func (a *aniLoggerAdapter) Warnf(format string, args ...any)  { a.logger.Warnf(format, args...) }
func (a *aniLoggerAdapter) Errorf(format string, args ...any) { a.logger.Errorf(format, args...) }
