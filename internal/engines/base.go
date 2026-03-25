package engines

import (
	"context"

	"github.com/udbp/udbproxy/pkg/types"
)

type BaseEngine struct {
	name string
}

func (e *BaseEngine) Name() string {
	return e.name
}

func (e *BaseEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

func (e *BaseEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

type EnginePipeline struct {
	engines []types.Engine
}

func NewEnginePipeline(engines ...types.Engine) *EnginePipeline {
	return &EnginePipeline{engines: engines}
}

func (p *EnginePipeline) AddEngine(engine types.Engine) {
	p.engines = append(p.engines, engine)
}

func (p *EnginePipeline) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	for _, engine := range p.engines {
		result := engine.Process(ctx, qc)
		if !result.Continue {
			return result
		}
	}
	return types.EngineResult{Continue: true}
}

func (p *EnginePipeline) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	for _, engine := range p.engines {
		result := engine.ProcessResponse(ctx, qc)
		if !result.Continue {
			return result
		}
	}
	return types.EngineResult{Continue: true}
}
