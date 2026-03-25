package plugin

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/udbp/udbproxy/pkg/types"
)

type Plugin struct {
	Name    string
	Version string
	Config  map[string]interface{}
	enabled bool
	runtime wazero.Runtime
	module  wazero.CompiledModule
}

type PluginContext struct {
	mu         sync.RWMutex
	plugins    map[string]*Plugin
	enginePool *sync.Pool
	runtime    wazero.Runtime
}

func NewPluginContext() *PluginContext {
	runtime := wazero.NewRuntimeWithConfig(context.Background(), wazero.NewRuntimeConfigCompiler())
	wasi_snapshot_preview1.MustInstantiate(context.Background(), runtime)

	return &PluginContext{
		plugins: make(map[string]*Plugin),
		runtime: runtime,
		enginePool: &sync.Pool{
			New: func() interface{} {
				return &PluginEngineContext{}
			},
		},
	}
}

func (pc *PluginContext) LoadPlugin(name, version string, wasmData []byte, config map[string]interface{}) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	compiled, err := pc.runtime.CompileModule(context.Background(), wasmData)
	if err != nil {
		return fmt.Errorf("failed to compile WASM module: %w", err)
	}

	plugin := &Plugin{
		Name:    name,
		Version: version,
		Config:  config,
		runtime: pc.runtime,
		module:  compiled,
		enabled: true,
	}

	pc.plugins[name] = plugin
	return nil
}

func (pc *PluginContext) UnloadPlugin(name string) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if plugin, ok := pc.plugins[name]; ok {
		plugin.module.Close(context.Background())
		delete(pc.plugins, name)
	}
	return nil
}

func (pc *PluginContext) GetPlugin(name string) (*Plugin, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	plugin, ok := pc.plugins[name]
	return plugin, ok
}

func (pc *PluginContext) ListPlugins() []*Plugin {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	plugins := make([]*Plugin, 0, len(pc.plugins))
	for _, p := range pc.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

func (pc *PluginContext) ExecutePlugin(ctx context.Context, name string, qc *types.QueryContext) types.EngineResult {
	plugin, ok := pc.GetPlugin(name)
	if !ok || !plugin.enabled {
		return types.EngineResult{Continue: true, Error: fmt.Errorf("plugin not found or disabled")}
	}

	engineCtx := pc.enginePool.Get().(*PluginEngineContext)
	defer pc.enginePool.Put(engineCtx)
	engineCtx.Context = ctx
	engineCtx.QueryContext = qc

	return types.EngineResult{Continue: true}
}

func (pc *PluginContext) Close() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	for _, plugin := range pc.plugins {
		plugin.module.Close(context.Background())
	}
	pc.plugins = make(map[string]*Plugin)

	return pc.runtime.Close(context.Background())
}

type PluginEngineContext struct {
	Context      context.Context
	QueryContext *types.QueryContext
}

type PluginLoader struct {
	pluginsDir string
	watchDir   bool
}

func NewPluginLoader(pluginsDir string) *PluginLoader {
	return &PluginLoader{
		pluginsDir: pluginsDir,
		watchDir:   false,
	}
}

func (pl *PluginLoader) LoadFromFile(ctx context.Context, filename string) (*Plugin, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin file: %w", err)
	}

	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	compiled, err := runtime.CompileModule(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to compile plugin: %w", err)
	}

	return &Plugin{
		Name:    filename,
		Version: "1.0.0",
		runtime: runtime,
		module:  compiled,
		enabled: true,
	}, nil
}

func (pl *PluginLoader) LoadFromRegistry(ctx context.Context, name, version string) (*Plugin, error) {
	return nil, fmt.Errorf("registry loading not implemented")
}

type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
	hooks   map[string][]PluginHook
}

type PluginHook func(ctx context.Context, qc *types.QueryContext) types.EngineResult

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]*Plugin),
		hooks:   make(map[string][]PluginHook),
	}
}

func (pr *PluginRegistry) Register(name string, plugin *Plugin) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.plugins[name] = plugin
}

func (pr *PluginRegistry) Unregister(name string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	delete(pr.plugins, name)
}

func (pr *PluginRegistry) Get(name string) (*Plugin, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	p, ok := pr.plugins[name]
	return p, ok
}

func (pr *PluginRegistry) RegisterHook(event string, hook PluginHook) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.hooks[event] = append(pr.hooks[event], hook)
}

func (pr *PluginRegistry) ExecuteHooks(ctx context.Context, event string, qc *types.QueryContext) types.EngineResult {
	pr.mu.RLock()
	hooks := pr.hooks[event]
	pr.mu.RUnlock()

	for _, hook := range hooks {
		result := hook(ctx, qc)
		if !result.Continue {
			return result
		}
	}
	return types.EngineResult{Continue: true}
}

func (p *Plugin) Enable() {
	p.enabled = true
}

func (p *Plugin) Disable() {
	p.enabled = false
}

func (p *Plugin) IsEnabled() bool {
	return p.enabled
}

func (p *Plugin) UpdateConfig(config map[string]interface{}) {
	p.Config = config
}

func (p *Plugin) GetConfig() map[string]interface{} {
	return p.Config
}

type PluginManager struct {
	ctx        *PluginContext
	registry   *PluginRegistry
	hotReload  bool
	reloadChan chan string
}

func NewPluginManager(hotReload bool) *PluginManager {
	return &PluginManager{
		ctx:        NewPluginContext(),
		registry:   NewPluginRegistry(),
		hotReload:  hotReload,
		reloadChan: make(chan string, 10),
	}
}

func (pm *PluginManager) LoadPlugin(ctx context.Context, name, version string, wasmData []byte, config map[string]interface{}) error {
	err := pm.ctx.LoadPlugin(name, version, wasmData, config)
	if err != nil {
		return err
	}

	plugin, _ := pm.ctx.GetPlugin(name)
	pm.registry.Register(name, plugin)

	return nil
}

func (pm *PluginManager) UnloadPlugin(name string) error {
	pm.registry.Unregister(name)
	return pm.ctx.UnloadPlugin(name)
}

func (pm *PluginManager) GetPlugin(name string) (*Plugin, bool) {
	return pm.ctx.GetPlugin(name)
}

func (pm *PluginManager) ListPlugins() []*Plugin {
	return pm.ctx.ListPlugins()
}

func (pm *PluginManager) ExecuteHooks(ctx context.Context, event string, qc *types.QueryContext) types.EngineResult {
	return pm.registry.ExecuteHooks(ctx, event, qc)
}

func (pm *PluginManager) StartHotReload() {
	if !pm.hotReload {
		return
	}
	go func() {
		for {
			select {
			case pluginName := <-pm.reloadChan:
				pm.reloadPlugin(pluginName)
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

func (pm *PluginManager) reloadPlugin(name string) {
	plugin, ok := pm.ctx.GetPlugin(name)
	if !ok {
		return
	}
	plugin.Disable()
	time.Sleep(100 * time.Millisecond)
	plugin.Enable()
}

func (pm *PluginManager) Close() error {
	return pm.ctx.Close()
}
