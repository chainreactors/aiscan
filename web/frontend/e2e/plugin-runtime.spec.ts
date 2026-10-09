import { test, expect, type Page } from '@playwright/test'
import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { anyPack } from '@bufbuild/protobuf/wkt'
import { AOPProtocolMessageSchema, EnvelopeSchema } from '@cyber/aop'

async function fixture(page: Page) {
  await page.goto('/e2e/fixtures/plugin-runtime.html')
  await page.waitForFunction(() => !!(window as any).runtimeFixture)
}

test('Cordis waits for injection, disposes consumers and reinstalls on reprovide', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const { Context } = (window as any).runtimeFixture
    const ctx = new Context(), log: string[] = []
    const consumer = ctx.plugin({ name: 'consumer', inject: ['example'], apply(scope: any) {
      log.push(`start:${scope.example}`)
      scope.effect(() => () => { log.push('stop') })
    } })
    await consumer
    const before = [...log]
    const provider = ctx.plugin({ name: 'provider', apply(scope: any) { scope.provide('example', 'one') } })
    await provider; await consumer
    const active = [...log]
    await provider.dispose(); await consumer
    const revoked = [...log]
    const replacement = ctx.plugin({ name: 'replacement', apply(scope: any) { scope.provide('example', 'two') } })
    await replacement; await consumer
    const reinstalled = [...log]
    await consumer.dispose(); await replacement.dispose()
    return { before, active, revoked, reinstalled }
  })
  expect(result).toEqual({ before: [], active: ['start:one'], revoked: ['start:one', 'stop'], reinstalled: ['start:one', 'stop', 'start:two'] })
})

test('isolated Hub tokens and separate roots never share services', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const { Context } = (window as any).runtimeFixture
    const root = new Context(), other = new Context()
    const left = root.isolate('example'), right = root.isolate('example')
    const values: string[] = []
    const consumer = { inject: ['example'], apply(ctx: any) { values.push(ctx.example) } }
    const consumers = [left.plugin(consumer), right.plugin(consumer), other.plugin(consumer)]
    const providers = [left, right, other].map((ctx, i) => ctx.plugin({ apply(scope: any) { scope.provide('example', String(i)) } }))
    await Promise.all(providers); await Promise.all(consumers)
    await providers[0].dispose()
    const unaffected = consumers.slice(1).map(item => item.state)
    await Promise.all(consumers.map(item => item.dispose()))
    await Promise.all(providers.map(item => item.dispose()))
    return { values, unaffected }
  })
  expect(result.values.sort()).toEqual(['0', '1', '2'])
  expect(result.unaffected).toEqual([2, 2])
})

test('Slots wait for declarations, retain order, reject duplicates and withdraw on disposal', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const { Slots, Context } = (window as any).runtimeFixture
    const ctx = new Context(), slots = new Slots(), renderer = { renderer: () => null }, views = () => null
    const pending = slots.contribute('conversation.extensions', 'recorded', renderer)
    const hidden = slots.resolve('recorded') === undefined
    const declare = slots.declare(ctx, 'conversation.extensions')
    const shown = !!slots.resolve('recorded')
    let duplicate = '', duplicateDeclaration = '', duplicateRoot = ''
    try { slots.contribute('conversation.extensions', 'recorded', renderer) } catch (error) { duplicate = String(error) }
    try { slots.declare(ctx, 'conversation.extensions') } catch (error) { duplicateDeclaration = String(error) }
    await declare(); const revoked = !slots.resolve('recorded')
    slots.declare(ctx, 'conversation.extensions'); const restored = !!slots.resolve('recorded')
    pending(); pending(); const removed = !slots.resolve('recorded')
    slots.declare(ctx, 'shell.header.actions')
    const last = slots.contribute('shell.header.actions', 'b', views, 20)
    slots.contribute('shell.header.actions', 'z', views, 10)
    slots.contribute('shell.header.actions', 'a', views, 10)
    const observable = slots.observe('shell.header.actions'), stable = observable.getSnapshot() === observable.getSnapshot()
    const order = observable.getSnapshot().map((item: any) => item.id)
    last(); const after = observable.getSnapshot().map((item: any) => item.id)
    slots.contribute('root', 'one', views)
    try { slots.contribute('root', 'two', views) } catch (error) { duplicateRoot = String(error) }
    return { hidden, shown, revoked, restored, removed, stable, order, after, duplicate, duplicateDeclaration, duplicateRoot }
  })
  expect(result).toMatchObject({ hidden: true, shown: true, revoked: true, restored: true, removed: true, stable: true, order: ['a', 'z', 'b'], after: ['a', 'z'] })
  expect(result.duplicate).toContain('Duplicate contribution')
  expect(result.duplicateDeclaration).toContain('already declared')
  expect(result.duplicateRoot).toContain('Duplicate contribution')
})

test('revoking a Slot declaration awaits dependent cleanup; reprovide runs contributions again', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const { Slots, Context } = (window as any).runtimeFixture
    const ctx = new Context(), slots = new Slots(), log: string[] = []
    const consumer = ctx.plugin({ inject: ['slot.shell.panels'], apply(scope: any) {
      log.push('install')
      scope.effect(() => {
        const remove = slots.contribute('shell.panels', 'owned', { component: () => null, mount: 'open' })
        return async () => { await Promise.resolve(); remove(); log.push('dispose') }
      })
    } })
    await consumer
    const waiting = [...log]
    const provide = () => ctx.plugin({ apply(scope: any) { scope.effect(() => slots.declare(scope, 'shell.panels')) } })
    const first = provide(); await first; await consumer
    const active = slots.observe('shell.panels').getSnapshot().map((entry: any) => entry.id)
    await first.dispose()
    const afterRevocation = [...log], revoked = slots.observe('shell.panels').getSnapshot()
    const second = provide(); await second; await consumer
    const restored = slots.observe('shell.panels').getSnapshot().map((entry: any) => entry.id)
    await consumer.dispose(); await second.dispose()
    return { waiting, active, afterRevocation, revoked, restored, log }
  })
  expect(result).toEqual({ waiting: [], active: ['owned'], afterRevocation: ['install', 'dispose'], revoked: [],
    restored: ['owned'], log: ['install', 'dispose', 'install', 'dispose'] })
})

test('a plugin adds a button, panel and renderer; uninstall/reinstall leaves no duplicates', async ({ page }) => {
  await fixture(page)
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.services = await f.startFixtureRuntime()
    f.plugin = f.services.context.plugin(f.samplePlugin)
    await f.plugin
    f.root = f.mountSample(f.services)
  })
  await page.getByRole('button', { name: 'Sample tool' }).click()
  await expect(page.getByTestId('sample-panel')).toBeVisible()
  await expect(page.getByTestId('sample-event')).toBeVisible()
  expect(await page.evaluate(() => !!(window as any).runtimeFixture.services.slots.resolve('sample_event'))).toBe(true)
  await page.evaluate(async () => { await (window as any).runtimeFixture.plugin.dispose() })
  await expect(page.getByRole('button', { name: 'Sample tool' })).toHaveCount(0)
  await expect(page.getByTestId('sample-panel')).toHaveCount(0)
  await expect(page.getByTestId('sample-event')).toHaveCount(0)
  await expect(page.locator('[data-unknown-extension="sample_event"]')).toBeVisible()
  expect(await page.evaluate(() => (window as any).runtimeFixture.services.panels.getSnapshot())).toBeNull()
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.plugin = f.services.context.plugin(f.samplePlugin); await f.plugin
  })
  await expect(page.getByRole('button', { name: 'Sample tool' })).toHaveCount(1)
  await expect(page.getByTestId('sample-event')).toHaveCount(1)
  await expect(page.getByTestId('sample-panel')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).runtimeFixture.services.slots.observe('conversation.extensions').getSnapshot().filter((entry: any) => entry.id === 'sample_event').length)).toBe(1)
})

test('historical scan, agent and JEV renderers survive missing live capabilities', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, services = await f.startFixtureRuntime()
    const types = ['scan_complete', 'agent_joined', 'jev_segment', 'jev_compilation', 'jev_check']
    const available = types.map(type => !!services.slots.resolve(type))
    const manifest = services.capabilities.getSnapshot()
    await services.dispose()
    return { available, advertised: manifest.advertised, after: types.map(type => !!services.slots.resolve(type)) }
  })
  expect(result).toEqual({ available: [true, true, true, true, true], advertised: [], after: [false, false, false, false, false] })
})

test('Scan protocol waits for advertising; disabling it preserves the shared connection', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, services = await f.startFixtureRuntime()
    let registrations = 0, closures = 0
    const register = services.connection.register, close = services.connection.close
    services.connection.register = (...args: any[]) => { registrations++; return register(...args) }
    services.connection.close = () => { closures++; close() }
    const consumer = services.context.plugin(f.ScanProtocolPlugin)
    await consumer
    const waiting = consumer.state
    const provide = () => services.context.plugin({ apply(ctx: any) { ctx.provide('scanAdvertised', true) } })
    const advertisement = provide(); await advertisement; await consumer
    const active = consumer.state
    await advertisement.dispose(); await consumer
    const disabled = consumer.state
    const replacement = provide(); await replacement; await consumer
    await consumer.dispose(); await replacement.dispose()
    const beforeClose = closures
    await services.dispose()
    return { waiting, active, disabled, registrations, beforeClose, closures }
  })
  expect(result).toEqual({ waiting: 0, active: 2, disabled: 0, registrations: 2, beforeClose: 0, closures: 1 })
})

test('Hub manifest and node capabilities stay separate from session permissions', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, auth = new f.AuthService()
    auth.set({ state: 'authenticated', generation: 3 })
    const connection = f.createConnection(auth, 3)
    connection.fetch = async () => new Response(JSON.stringify({ product: 'neutral', capabilities: [{ id: 'core' }] }))
    const capabilities = new f.CapabilityService(connection)
    await capabilities.refresh()
    capabilities.setNodes({ scanNode: ['scan', 'pty'], coreNode: ['core', 'file'] })
    const first = capabilities.getSnapshot()
    capabilities.setNodes({ coreNode: ['core'] })
    const second = capabilities.getSnapshot()
    capabilities.dispose(); connection.close()
    return { hub: first.manifest.capabilities, nodes: first.nodes, union: first.advertised, after: second.advertised }
  })
  expect(result).toEqual({ hub: [{ id: 'core' }], nodes: { scanNode: ['scan', 'pty'], coreNode: ['core', 'file'] }, union: ['core', 'scan'], after: ['core'] })
})

test('only manifest 404 enters compatibility mode; other failures retry', async ({ page }) => {
  await fixture(page)
  await page.clock.install()
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    const connection = { fetch: async () => new Response('', { status: 404 }) }
    f.compat = new f.CapabilityService(connection); await f.compat.refresh()
    let requests = 0
    f.retry = new f.CapabilityService({ fetch: async () => ++requests === 1
      ? new Response('', { status: 503 }) : new Response(JSON.stringify({ product: 'recovered', capabilities: [{ id: 'ioa' }] })) })
    await f.retry.refresh()
  })
  expect(await page.evaluate(() => (window as any).runtimeFixture.compat.getSnapshot().manifest.capabilities)).toEqual([{ id: 'core' }])
  expect(await page.evaluate(() => (window as any).runtimeFixture.retry.getSnapshot().manifest)).toBeNull()
  await page.clock.fastForward(3001)
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.retry.getSnapshot().manifest?.product)).toBe('recovered')
})

test('a stale HTTP or RPC 401 cannot invalidate the new authentication generation', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, auth = new f.AuthService()
    auth.set({ state: 'authenticated', generation: 1 })
    const connection = f.createConnection(auth, 1)
    const originalFetch = window.fetch
    let respond: any
    window.fetch = () => new Promise(resolve => { respond = resolve })
    const request = connection.fetch('/api/old')
    auth.set({ state: 'authenticated', generation: 2 })
    respond(new Response('', { status: 401 })); await request.catch(() => {})
    // Start a real ConnectRPC call in the old generation, then hold its 401.
    auth.set({ state: 'authenticated', generation: 1 })
    const rpc = connection.rpc.system.getStatus({}).catch(() => {})
    await Promise.resolve(); await Promise.resolve()
    auth.set({ state: 'authenticated', generation: 2 })
    respond(new Response(JSON.stringify({ code: 'unauthenticated', message: 'old request' }), {
      status: 401, headers: { 'Content-Type': 'application/json' },
    }))
    await rpc
    auth.invalidate(1)
    const snapshot = auth.getSnapshot()
    window.fetch = originalFetch; connection.close(); auth.dispose()
    return snapshot
  })
  expect(result).toEqual({ state: 'authenticated', generation: 2 })
})

test('unknown extension remains visible and never invokes cyber-ui global business renderers', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(() => {
    const { withWorkflows, Slots } = (window as any).runtimeFixture
    const item = { id: 'unknown', kind: 'extension', extensionType: 'third_party_event', timestamp: 1, data: { text: 'keep' } }
    return withWorkflows([item], [], new Slots().resolve)
  })
  expect(result).toEqual([{ id: 'unknown', kind: 'extension', extensionType: 'third_party_event', timestamp: 1, data: { text: 'keep' } }])
})

test('StrictMode and repeated start use one runtime; logout awaits ordered cleanup', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/auth/logout', route => route.fulfill({ json: {} }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [{ id: 'core' }] } }))
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, log: string[] = []
    const runtime = new f.AppRuntime([{ name: 'async-consumer', inject: ['connection'], apply(ctx: any) {
      const connection = ctx.connection, original = connection.close
      connection.close = () => { log.push('close'); original() }
      ctx.effect(() => async () => { await Promise.resolve(); log.push('consumer') })
    } }])
    const container = document.getElementById('root')!
    const first = runtime.start(container), second = runtime.start(container)
    await first; await second
    const diagnostics = runtime.diagnostics()
    await runtime.auth.logout()
    const afterLogout = runtime.hub.getSnapshot()
    const dispose = runtime.dispose()
    const sameDisposal = dispose === runtime.dispose()
    await dispose
    return { sameStart: first === second, sameDisposal, log, afterLogout,
      roots: diagnostics.filter((item: any) => item.name === 'react-renderer').length,
      connections: diagnostics.filter((item: any) => item.name === 'connection').length,
      remaining: runtime.diagnostics().length }
  })
  expect(result).toEqual({ sameStart: true, sameDisposal: true, log: ['consumer', 'close'], afterLogout: null, roots: 1, connections: 1, remaining: 0 })
})

test('runtime replacement awaits disposal before opening the next authenticated Hub', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  const result = await page.evaluate(async () => {
    const { AppRuntime } = (window as any).runtimeFixture
    const first = new AppRuntime([]), second = new AppRuntime([]), root = document.getElementById('root')!
    await first.start(root)
    await first.dispose()
    await second.start(root)
    const generation = second.hub.getSnapshot().connection.generation
    const connections = second.diagnostics().filter((item: any) => item.name === 'connection').length
    await second.dispose()
    return { generation, connections, firstRemaining: first.diagnostics().length, secondRemaining: second.diagnostics().length }
  })
  expect(result).toEqual({ generation: 1, connections: 1, firstRemaining: 0, secondRemaining: 0 })
})

test('two plugin consumers and repeated start share one physical AOP socket', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/auth/logout', route => route.fulfill({ json: {} }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, sockets: any[] = [], log: string[] = []
    class Socket {
      static OPEN = 1
      readyState = 0; binaryType = ''; onopen?: () => void; onclose?: () => void
      constructor() { sockets.push(this); queueMicrotask(() => { this.readyState = 1; this.onopen?.() }) }
      send() {}
      close() { this.readyState = 3; log.push('socket-close'); this.onclose?.() }
    }
    window.WebSocket = Socket as any
    const consumer = (name: string) => ({ name, inject: ['connection'], async apply(ctx: any) {
      const connection = ctx.connection
      ctx.effect(() => connection.aop.onConnectionChange(() => {}))
      await connection.aop.connect()
    } })
    const runtime = new f.AppRuntime([consumer('one'), consumer('two')])
    const start = runtime.start(document.getElementById('root')!)
    await runtime.start(document.getElementById('root')!); await start
    // Dynamic children belong to the Hub too, including async cleanup.
    const owner = [...runtime.context.registry.values()].find((item: any) => item.name === 'authenticated-hub') as any
    const dynamic = [...owner.fibers][0].ctx.plugin({
      name: 'dynamic-consumer', inject: ['connection'], apply(ctx: any) {
        ctx.effect(() => async () => { await Promise.resolve(); log.push('dynamic-dispose') })
      },
    })
    await dynamic
    await runtime.auth.logout()
    const afterLogout = runtime.hub.getSnapshot()
    await runtime.dispose()
    return { sockets: sockets.length, states: sockets.map(socket => socket.readyState), log, afterLogout }
  })
  expect(result).toEqual({ sockets: 1, states: [3], log: ['dynamic-dispose', 'socket-close'], afterLogout: null })
})

test('login waits for a pending logout request before creating the next generation', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const { AuthService } = (window as any).runtimeFixture, auth = new AuthService()
    auth.set({ state: 'authenticated', generation: 1 })
    const previous = window.fetch, calls: string[] = []
    let release: () => void = () => {}
    window.fetch = async input => {
      calls.push(String(input))
      if (String(input).endsWith('/logout')) await new Promise<void>(resolve => { release = resolve })
      return new Response('{}')
    }
    const logout = auth.logout(), duplicate = auth.logout(), login = auth.login('fixture-token')
    await Promise.resolve()
    const waiting = [...calls]
    release(); await logout; await login
    const snapshot = auth.getSnapshot()
    window.fetch = previous; auth.dispose()
    return { duplicate: logout === duplicate, waiting, calls, snapshot }
  })
  expect(result).toEqual({ duplicate: true, waiting: ['/api/auth/logout'], calls: ['/api/auth/logout', '/api/auth/login'], snapshot: { state: 'authenticated', generation: 3 } })
})

test('logout releases a mounted PTY terminal after API admission stops', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/auth/logout', route => route.fulfill({ json: {} }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.sockets = []
    class Socket {
      static OPEN = 1
      readyState = 0; binaryType = ''; frames: Uint8Array[] = []; onopen?: () => void; onclose?: () => void
      constructor() { f.sockets.push(this); queueMicrotask(() => { this.readyState = 1; this.onopen?.() }) }
      send(frame: Uint8Array) { this.frames.push(frame) }
      close() { this.readyState = 3; this.onclose?.() }
    }
    window.WebSocket = Socket as any
    f.runtime = new f.AppRuntime([f.TerminalFixtureUiPlugin])
    await f.runtime.start(document.getElementById('root')!)
  })
  await expect(page.locator('.xterm')).toBeVisible()
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.sockets[0]?.frames.length || 0)).toBeGreaterThan(0)
  await page.evaluate(async () => { await (window as any).runtimeFixture.runtime.auth.logout() })
  await expect(page.locator('.xterm')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).runtimeFixture.sockets.map((socket: any) => socket.readyState))).toEqual([3])
  await page.evaluate(async () => { await (window as any).runtimeFixture.runtime.dispose() })
  expect(errors).toEqual([])
})

test('a partial plugin failure rolls back its resources and the Hub; retry recovers visibly', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.log = []; let fail = true
    f.runtime = new f.AppRuntime([{
      name: 'recoverable-ui', inject: ['slots', 'connection'], apply(ctx: any) {
        const connection = ctx.connection, close = connection.close
        connection.close = () => { f.log.push('close'); close() }
        ctx.effect(() => ctx.slots.declare(ctx, 'root'))
        ctx.effect(() => ctx.slots.contribute('root', 'ready', () => 'Recovered workbench'))
        ctx.effect(() => async () => { await Promise.resolve(); f.log.push('cleanup') })
        if (fail) { fail = false; throw new Error('fixture plugin failed') }
      },
    }])
    await f.runtime.start(document.getElementById('root')!)
    f.afterFailure = { log: [...f.log], hub: f.runtime.hub.getSnapshot(), errors: f.runtime.errors.getSnapshot(),
      hubFibers: f.runtime.diagnostics().filter((item: any) => item.name === 'authenticated-hub').length }
  })
  await expect(page.getByRole('alert')).toBeVisible()
  expect(await page.evaluate(() => (window as any).runtimeFixture.afterFailure)).toEqual({
    log: ['cleanup', 'close'], hub: null, errors: ['Error: fixture plugin failed'], hubFibers: 0,
  })
  await page.getByRole('button').click()
  await expect(page.getByText('Recovered workbench')).toBeVisible()
  expect(await page.evaluate(() => (window as any).runtimeFixture.runtime.errors.getSnapshot())).toEqual([])
  await page.evaluate(async () => { await (window as any).runtimeFixture.runtime.dispose() })
  expect(await page.evaluate(() => (window as any).runtimeFixture.log)).toEqual(['cleanup', 'close', 'cleanup', 'close'])
})

test('disposal during authentication startup leaves no bridge, Fiber or React root', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, original = window.fetch
    let began: () => void = () => {}, release: (response: Response) => void = () => {}
    const requested = new Promise<void>(resolve => { began = resolve })
    window.fetch = () => { began(); return new Promise<Response>(resolve => { release = resolve }) }
    const runtime = new f.AppRuntime([]), root = document.getElementById('root')!
    const start = runtime.start(root)
    await requested
    const dispose = runtime.dispose()
    release(new Response(JSON.stringify({ authenticated: true })))
    await start; await dispose
    let bridgeBlocked = false
    try { f.aopClient.onConnectionChange(() => {}) } catch { bridgeBlocked = true }
    window.fetch = original
    return { hub: runtime.hub.getSnapshot(), fibers: runtime.diagnostics().length, children: root.childElementCount, bridgeBlocked }
  })
  expect(result).toEqual({ hub: null, fibers: 0, children: 0, bridgeBlocked: true })
})

test('burst HMR replacements wait for pending cleanup and only start the final runtime', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, log: string[] = [], root = document.getElementById('root')!
    let release: () => void = () => {}, began: () => void = () => {}
    const disposing = new Promise<void>(resolve => { began = resolve })
    const first = new f.AppRuntime([{ name: 'first', apply(ctx: any) {
      log.push('first-start')
      ctx.effect(() => async () => { began(); await new Promise<void>(resolve => { release = resolve }); log.push('first-stop') })
    } }])
    const second = new f.AppRuntime([{ name: 'second', apply() { log.push('second-start') } }])
    const third = new f.AppRuntime([{ name: 'third', apply() { log.push('third-start') } }])
    await f.mountBrowserRuntime(first, root)
    const firstDisposal = f.unmountBrowserRuntime(first)
    await disposing
    const middle = f.mountBrowserRuntime(second, root)
    const latest = f.mountBrowserRuntime(third, root)
    const waiting = [...log]
    release(); await firstDisposal; await middle; await latest
    const owner = window.__CYBER_APP_RUNTIME__ === third
    await f.unmountBrowserRuntime(third)
    return { waiting, log, owner, remaining: [first, second, third].map(runtime => runtime.diagnostics().length) }
  })
  expect(result).toEqual({ waiting: ['first-start'], log: ['first-start', 'first-stop', 'third-start'], owner: true, remaining: [0, 0, 0] })
})

test('a disposed shared AOP handle cannot recreate its socket', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, auth = new f.AuthService(), sockets: any[] = []
    class Socket {
      static OPEN = 1
      readyState = 0; onopen?: () => void; onclose?: () => void
      constructor() { sockets.push(this); queueMicrotask(() => { this.readyState = 1; this.onopen?.() }) }
      send() {}
      close() { this.readyState = 3; this.onclose?.() }
    }
    window.WebSocket = Socket as any
    auth.set({ state: 'authenticated', generation: 1 })
    const connection = f.createConnection(auth, 1), handle = connection.aop
    const listener = handle.onConnectionChange(() => {})
    await handle.connect(); connection.close(); listener()
    let blocked = false
    await handle.connect().catch(() => { blocked = true })
    auth.dispose()
    return { blocked, connected: handle.connected, sockets: sockets.length, states: sockets.map(socket => socket.readyState) }
  })
  expect(result).toEqual({ blocked: true, connected: false, sockets: 1, states: [3] })
})

test('manifest refresh deduplicates in-flight calls and owns a stable node snapshot', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    let calls = 0, release: (response: Response) => void = () => {}
    const capabilities = new f.CapabilityService({ fetch() { calls++; return new Promise<Response>(resolve => { release = resolve }) } })
    const first = capabilities.refresh(), second = capabilities.refresh()
    const nodes = { node: ['scan', 'pty'] }; capabilities.setNodes(nodes)
    nodes.node.push('ioa')
    const before = capabilities.getSnapshot()
    release(new Response(JSON.stringify({ product: 'test', capabilities: [{ id: 'core' }] })))
    await first; await second
    const after = capabilities.getSnapshot()
    capabilities.dispose(); capabilities.setNodes({ late: ['ioa'] })
    return { sameRefresh: first === second, calls, before: before.nodes, after: after.advertised,
      stopped: capabilities.getSnapshot() === after }
  })
  expect(result).toEqual({ sameRefresh: true, calls: 1, before: { node: ['scan', 'pty'] }, after: ['core', 'scan'], stopped: true })
})

test('re-authentication revokes the old generation before the login request finishes', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, auth = new f.AuthService(), original = window.fetch
    auth.set({ state: 'authenticated', generation: 1 })
    const connection = f.createConnection(auth, 1)
    let release: (response: Response) => void = () => {}
    window.fetch = () => new Promise<Response>(resolve => { release = resolve })
    const login = auth.login('new-token'); await Promise.resolve()
    const pending = auth.getSnapshot()
    let blocked = false
    await connection.fetch('/api/old').catch(() => { blocked = true })
    release(new Response('{}')); await login
    const snapshot = auth.getSnapshot()
    window.fetch = original; connection.close(); auth.dispose()
    return { pending, blocked, snapshot }
  })
  expect(result).toEqual({ pending: { state: 'checking', generation: 2 }, blocked: true, snapshot: { state: 'authenticated', generation: 2 } })
})

test('a failed competing browser runtime rolls back without stopping the existing API bridge', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, root = document.getElementById('root')!
    const owner = new f.AppRuntime([]), competing = new f.AppRuntime([])
    await owner.start(root)
    let failed = false
    await competing.start(document.createElement('div')).catch(() => { failed = true })
    const rolledBack = competing.diagnostics().length
    await competing.dispose()
    const release = f.aopClient.onConnectionChange(() => {})
    release()
    const ownerConnections = owner.diagnostics().filter((entry: any) => entry.name === 'connection').length
    await owner.dispose()
    const replacement = new f.AppRuntime([])
    await replacement.start(root); await replacement.dispose()
    return { failed, rolledBack, ownerConnections, remaining: replacement.diagnostics().length }
  })
  expect(result).toEqual({ failed: true, rolledBack: 0, ownerConnections: 1, remaining: 0 })
})

test('AOP responses from a revoked generation cannot complete a request or reach its subscriptions', async ({ page }) => {
  const replies: Array<() => void> = []
  await page.routeWebSocket('**/api/aop/application/ws', socket => socket.onMessage(data => {
    const request = fromBinary(EnvelopeSchema, data as Buffer)
    if (!['old-request', 'old-stream'].includes(request.id)) return
    replies.push(() => socket.send(Buffer.from(toBinary(EnvelopeSchema, create(EnvelopeSchema, {
      replyTo: request.id, payload: anyPack(AOPProtocolMessageSchema, create(AOPProtocolMessageSchema)),
    })))))
  }))
  await fixture(page)
  await page.evaluate(() => {
    const f = (window as any).runtimeFixture
    f.auth = new f.AuthService(); f.auth.set({ state: 'authenticated', generation: 1 })
    f.connection = f.createConnection(f.auth, 1); f.deliveries = 0
    f.unsubscribe = f.connection.aop.subscribe(f.AOPProtocolMessageSchema, f.create(f.AOPProtocolMessageSchema), () => { f.deliveries++ }, { id: 'old-stream' })
    f.result = f.connection.aop.request(f.AOPProtocolMessageSchema, f.create(f.AOPProtocolMessageSchema), { id: 'old-request' })
      .then(() => 'delivered', (error: Error) => error.message)
  })
  await expect.poll(() => replies.length).toBe(2)
  await page.evaluate(() => (window as any).runtimeFixture.auth.set({ state: 'authenticated', generation: 2 }))
  replies.forEach(reply => reply())
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, request = await f.result
    f.unsubscribe(); f.connection.close(); f.auth.dispose()
    return { request, deliveries: f.deliveries }
  })
  expect(result).toEqual({ request: 'Connection is inactive', deliveries: 0 })
})

test('capability disposal aborts and drains an in-flight manifest request', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, log: string[] = []
    const capabilities = new f.CapabilityService({ fetch(_input: unknown, init: RequestInit) {
      return new Promise<Response>((_resolve, reject) => init.signal!.addEventListener('abort', () => {
        log.push('abort'); queueMicrotask(() => { log.push('settled'); reject(new DOMException('aborted', 'AbortError')) })
      }, { once: true }))
    } })
    const refresh = capabilities.refresh()
    await capabilities.dispose(); log.push('disposed'); await refresh
    return { log, error: capabilities.getSnapshot().error }
  })
  expect(result).toEqual({ log: ['abort', 'settled', 'disposed'], error: null })
})

test('a stale authentication check reports false and cannot replace the newer state', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, auth = new f.AuthService(), original = window.fetch
    let release: (response: Response) => void = () => {}
    window.fetch = () => new Promise<Response>(resolve => { release = resolve })
    const check = auth.check(); await Promise.resolve()
    auth.set({ state: 'unauthenticated', generation: 2 })
    release(new Response(JSON.stringify({ authenticated: true })))
    const authenticated = await check, snapshot = auth.getSnapshot()
    window.fetch = original; auth.dispose()
    return { authenticated, snapshot }
  })
  expect(result).toEqual({ authenticated: false, snapshot: { state: 'unauthenticated', generation: 2 } })
})

test('a failed panel or timeline renderer stays local and recovers after plugin replacement', async ({ page }) => {
  await fixture(page)
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.services = await f.startFixtureRuntime()
    const Broken = () => { throw new Error('optional view failed') }
    f.broken = f.services.context.plugin({ inject: ['slots', 'slot.shell.panels', 'slot.conversation.extensions'], apply(ctx: any) {
      ctx.effect(() => ctx.slots.contribute('shell.panels', 'broken', { component: Broken, mount: 'open' }))
      ctx.effect(() => ctx.slots.contribute('conversation.extensions', 'sample_event', { renderer: Broken }))
    } })
    await f.broken
    f.services.panels.open('broken')
    f.root = f.mountSample(f.services)
  })
  await expect(page.locator('[data-failed-contribution="broken"]')).toBeVisible()
  await expect(page.locator('[data-failed-contribution="sample_event"]')).toBeVisible()
  await expect(page.locator('textarea')).toBeVisible()
  await page.locator('textarea').fill('keep this draft')
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    await f.broken.dispose()
    f.plugin = f.services.context.plugin(f.samplePlugin); await f.plugin
  })
  await expect(page.getByTestId('sample-event')).toBeVisible()
  await expect(page.locator('textarea')).toHaveValue('keep this draft')
  await expect(page.locator('[data-failed-contribution]')).toHaveCount(0)
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture; f.root.unmount(); await f.services.dispose()
  })
})

test('fixture Hub disposal is idempotent and releases dynamic children and unload listeners', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, listeners = new Set<EventListenerOrEventListenerObject>()
    const add = window.addEventListener, remove = window.removeEventListener
    window.addEventListener = function(type: any, listener: any, options: any) {
      if (type === 'beforeunload') listeners.add(listener)
      return add.call(this, type, listener, options)
    } as any
    window.removeEventListener = function(type: any, listener: any, options: any) {
      if (type === 'beforeunload') listeners.delete(listener)
      return remove.call(this, type, listener, options)
    } as any
    const services = await f.startFixtureRuntime(), log: string[] = []
    services.workbench.set({ stale: true }); services.panels.open('sample')
    const child = services.context.plugin({ apply(ctx: any) { ctx.effect(() => async () => { await Promise.resolve(); log.push('child') }) } })
    await child
    const first = services.dispose(), second = services.dispose(); await first; await second
    const replacement = await f.startFixtureRuntime(); await replacement.dispose()
    window.addEventListener = add; window.removeEventListener = remove
    return { same: first === second, log, listeners: listeners.size, workbench: services.workbench.getSnapshot(),
      panels: services.panels.getSnapshot(), fibers: services.context.registry.size }
  })
  expect(result).toEqual({ same: true, log: ['child'], listeners: 0, workbench: null, panels: null, fibers: 0 })
})

test('the Scan feature follows capability changes without affecting archive renderers', async ({ page }) => {
  await fixture(page)
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.services = await f.startFixtureRuntime()
    f.provider = f.services.context.plugin({ apply(ctx: any) { ctx.provide('capabilities', f.services.capabilities) } })
    await f.provider
    f.protocol = f.services.context.plugin(f.ScanProtocolPlugin)
    f.discovery = f.services.context.plugin(f.ScanAdvertisementPlugin)
    await f.discovery; await f.protocol
  })
  expect(await page.evaluate(() => (window as any).runtimeFixture.protocol.state)).toBe(0)
  await page.evaluate(() => (window as any).runtimeFixture.services.capabilities.setNodes({ scanner: ['scan', 'pty'] }))
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.protocol.state)).toBe(2)
  await page.evaluate(() => (window as any).runtimeFixture.services.capabilities.setNodes({ core: ['core', 'pty'] }))
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.protocol.state)).toBe(0)
  expect(await page.evaluate(() => !!(window as any).runtimeFixture.services.slots.resolve('scan_complete'))).toBe(true)
  await page.evaluate(() => (window as any).runtimeFixture.services.capabilities.setNodes({ scanner: ['scan'] }))
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.protocol.state)).toBe(2)
  await page.evaluate(async () => { await (window as any).runtimeFixture.services.dispose() })
})

test('browser replacement waits for the old logout response before checking authentication again', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'test', capabilities: [] } }))
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture, original = window.fetch
    f.authChecks = 0
    window.fetch = async (input, init) => {
      if (String(input).endsWith('/logout')) {
        await new Promise<void>(resolve => { f.releaseLogout = resolve })
        return new Response('{}')
      }
      if (String(input).endsWith('/session')) f.authChecks++
      return original(input, init)
    }
    const root = document.getElementById('root')!
    f.first = new f.AppRuntime([]); f.second = new f.AppRuntime([])
    await f.mountBrowserRuntime(f.first, root)
    f.logout = f.first.auth.logout()
    f.replacement = f.mountBrowserRuntime(f.second, root)
  })
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.first.hub.getSnapshot())).toBeNull()
  expect(await page.evaluate(() => (window as any).runtimeFixture.authChecks)).toBe(1)
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.releaseLogout(); await f.logout; await f.replacement
  })
  expect(await page.evaluate(() => (window as any).runtimeFixture.authChecks)).toBe(2)
  await page.evaluate(async () => { await (window as any).runtimeFixture.unmountBrowserRuntime((window as any).runtimeFixture.second) })
})


test('published workbench composition retains asset and tool navigation and releases its Hub', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await fixture(page)
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/api/auth/logout', route => route.fulfill({ json: {} }))
  await page.route('**/api/manifest', route => route.fulfill({ json: { product: 'cyber-harness', capabilities: [] } }))
  await page.route('**/cyber.rpc.*/**', route => route.fulfill({ contentType: 'application/proto', body: Buffer.alloc(0) }))
  await page.route('**/ioa/**', route => route.fulfill({ json: [] }))
  await page.evaluate(async () => {
    const f = (window as any).runtimeFixture
    f.runtime = new f.AppRuntime()
    await f.runtime.start(document.getElementById('root')!)
  })
  await page.locator('[data-ui-guide="assets"]').click()
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.runtime.hub.getSnapshot()?.panels.getSnapshot()?.id)).toBe('assets')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.locator('[data-ui-guide="tools"]').click()
  await expect.poll(() => page.evaluate(() => (window as any).runtimeFixture.runtime.hub.getSnapshot()?.panels.getSnapshot()?.id)).toBe('tools')
  await expect(page.getByRole('dialog')).toHaveCount(1)
  await page.evaluate(async () => { await (window as any).runtimeFixture.runtime.auth.logout() })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const remaining = await page.evaluate(async () => {
    const runtime = (window as any).runtimeFixture.runtime
    await runtime.dispose()
    return runtime.diagnostics().length
  })
  expect(remaining).toBe(0)
  expect(errors).toEqual([])
})
