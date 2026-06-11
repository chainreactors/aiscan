package zombie

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/sdk/pkg/types"
	zombiecore "github.com/chainreactors/zombie/core"
	zombiepkg "github.com/chainreactors/zombie/pkg"
)

type Engine struct {
	inited   bool
	config   *Config
	capacity *types.Capacity
	mu       sync.Mutex
}

func NewEngine(config *Config) *Engine {
	if config == nil {
		config = NewConfig()
	}
	e := &Engine{config: config}
	if config.Capacity > 0 {
		e.capacity = types.NewCapacity(config.Capacity)
	}
	return e
}

func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.inited {
		return nil
	}
	if e.config != nil && e.config.ResourceProvider != nil {
		zombiepkg.SetResourceProvider(e.config.ResourceProvider)
	}
	if err := zombiepkg.Load(); err != nil {
		return err
	}
	e.inited = true
	return nil
}

func (e *Engine) Name() string {
	return "zombie"
}

func (e *Engine) SetCapacity(total int) {
	if total > 0 {
		e.capacity = types.NewCapacity(total)
	}
}

func (e *Engine) Capacity() *types.Capacity {
	return e.capacity
}

func (e *Engine) Execute(ctx types.Context, task types.Task) (<-chan types.Result, error) {
	if !e.inited {
		if err := e.Init(); err != nil {
			return nil, err
		}
	}
	if err := task.Validate(); err != nil {
		return nil, err
	}

	var runCtx *Context
	if ctx == nil {
		runCtx = NewContext()
	} else {
		var ok bool
		runCtx, ok = ctx.(*Context)
		if !ok {
			return nil, fmt.Errorf("unsupported context type: %T", ctx)
		}
	}

	switch t := task.(type) {
	case *BruteTask:
		return e.execute(runCtx, t)
	default:
		return nil, fmt.Errorf("unsupported task type: %s", task.Type())
	}
}

func (e *Engine) Close() error {
	return nil
}

// ========================================
// 便捷 API — 按攻击模式分类
// ========================================

// Brute runs clusterbomb mode: cartesian product of users × passwords.
func (e *Engine) Brute(ctx *Context, targets []Target, users, passwords []string) ([]*types.ZombieResult, error) {
	return e.collect(e.BruteStream(ctx, targets, users, passwords))
}

func (e *Engine) BruteStream(ctx *Context, targets []Target, users, passwords []string) (<-chan *types.ZombieResult, error) {
	task := &BruteTask{
		Targets:   targets,
		Users:     users,
		Passwords: passwords,
		mod:       types.ZombieModeBomb,
	}
	return e.typedStream(ctx, task)
}

// Pitchfork runs pitchfork mode: paired username::password auth list.
func (e *Engine) Pitchfork(ctx *Context, targets []Target, auths []Auth) ([]*types.ZombieResult, error) {
	return e.collect(e.PitchforkStream(ctx, targets, auths))
}

func (e *Engine) PitchforkStream(ctx *Context, targets []Target, auths []Auth) (<-chan *types.ZombieResult, error) {
	task := &BruteTask{
		Targets: targets,
		Auths:   auths,
		mod:     types.ZombieModePitchFork,
	}
	return e.typedStream(ctx, task)
}

// Sniper runs sniper mode: one attempt per target using its own credentials.
func (e *Engine) Sniper(ctx *Context, targets []Target) ([]*types.ZombieResult, error) {
	return e.collect(e.SniperStream(ctx, targets))
}

func (e *Engine) SniperStream(ctx *Context, targets []Target) (<-chan *types.ZombieResult, error) {
	task := &BruteTask{
		Targets: targets,
		mod:     types.ZombieModeSniper,
	}
	return e.typedStream(ctx, task)
}

// ========================================
// 内部执行
// ========================================

func (e *Engine) execute(ctx *Context, task *BruteTask) (<-chan types.Result, error) {
	threads := ctx.opt.Threads
	if e.capacity != nil {
		if err := e.capacity.Acquire(ctx.Context(), threads); err != nil {
			return nil, err
		}
	}

	opt := ctx.opt.Clone()
	opt.Quiet = true
	opt.NoCheckHoneyPot = true
	if task.mod != "" {
		opt.Mod = task.mod
	}
	if opt.Mod == "" {
		opt.Mod = types.ZombieModeBomb
	}

	// 解析代理：Context > Config。Client 级代理在 ensureZombie 时已下沉到 config。
	if proxies := types.ResolveProxy(ctx.proxy, e.config.Proxy); len(proxies) > 0 {
		dialer, err := types.NewProxyDialer(proxies)
		if err != nil {
			if e.capacity != nil {
				e.capacity.Release(threads)
			}
			return nil, fmt.Errorf("apply proxy failed: %v", err)
		}
		opt.ProxyDial = dialer.DialContext
	}

	runner := zombiecore.NewRunner(opt)
	runner.SetTargets(convertTargets(task.Targets))
	if len(task.Users) > 0 {
		runner.SetUsers(task.Users)
	}
	if len(task.Passwords) > 0 {
		runner.SetPasswords(task.Passwords)
	}
	if len(task.Auths) > 0 {
		pairs := make([]string, len(task.Auths))
		for i, a := range task.Auths {
			pairs[i] = a.Username + "::" + a.Password
		}
		runner.SetAuths(pairs)
	}

	started := time.Now()
	resultCh := make(chan types.Result, threads)

	go func() {
		defer close(resultCh)
		if e.capacity != nil {
			defer e.capacity.Release(threads)
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			for result := range runner.OutputCh {
				select {
				case resultCh <- types.NewResult(result.OK, result.Err, result.ZombieResult):
				case <-ctx.Context().Done():
					return
				}
			}
		}()

		_ = runner.RunWithContext(ctx.Context())
		<-done

		stat := runner.Stat()
		ctx.emitStats(types.Stats{
			Engine:   e.Name(),
			Task:     task.Type(),
			Targets:  int64(len(task.Targets)),
			Tasks:    int64(stat.Total),
			Requests: int64(stat.Total),
			Results:  int64(stat.Success),
			Duration: time.Since(started),
		})
	}()

	return resultCh, nil
}

func (e *Engine) typedStream(ctx *Context, task *BruteTask) (<-chan *types.ZombieResult, error) {
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	zombieResultCh := make(chan *types.ZombieResult, 100)
	go func() {
		defer close(zombieResultCh)
		for result := range resultCh {
			if data, ok := types.ResultData[*types.ZombieResult](result); result.Success() && ok && data != nil {
				zombieResultCh <- data
			}
		}
	}()
	return zombieResultCh, nil
}

func (e *Engine) collect(ch <-chan *types.ZombieResult, err error) ([]*types.ZombieResult, error) {
	if err != nil {
		return nil, err
	}
	var results []*types.ZombieResult
	for result := range ch {
		results = append(results, result)
	}
	return results, nil
}

func convertTargets(targets []Target) []*Target {
	result := make([]*Target, 0, len(targets))
	for i := range targets {
		t := normalizeTarget(targets[i])
		if t.Service == "" {
			continue
		}
		result = append(result, &t)
	}
	return result
}

func normalizeTarget(target Target) Target {
	target.Service = strings.ToLower(strings.TrimSpace(target.Service))
	if target.Service == "" {
		return target
	}
	if service, ok := zombiepkg.Services.Get(target.Service); ok {
		target.Service = service.Name
	}
	if target.Port == "" {
		target.Port = zombiepkg.Services.DefaultPort(target.Service)
	}
	if target.Scheme == "" {
		target.Scheme = target.Service
	}
	return target
}
