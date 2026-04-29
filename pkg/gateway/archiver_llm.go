// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package gateway

import (
	"context"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// archiverLLMAdapter bridges archiver.LLMClient to pkg/providers.
//
// This adapter lives in pkg/gateway (not pkg/archiver) to avoid an import
// cycle: pkg/config will import pkg/archiver, so pkg/archiver must NOT
// import pkg/config or any package that depends on it. pkg/gateway already
// depends on both pkg/config and pkg/providers, so it is the natural seam.
type archiverLLMAdapter struct {
	cfg       *config.Config
	modelName string
}

// newArchiverLLMAdapter constructs an adapter that resolves a provider from
// the supplied config. If modelName is empty, the agent default model is used.
func newArchiverLLMAdapter(cfg *config.Config, modelName string) *archiverLLMAdapter {
	return &archiverLLMAdapter{cfg: cfg, modelName: modelName}
}

// Distill issues a one-shot chat completion against the configured provider.
// It satisfies archiver.LLMClient.
func (a *archiverLLMAdapter) Distill(ctx context.Context, prompt string) (string, error) {
	if a.cfg == nil {
		return "", fmt.Errorf("archiver llm adapter: nil config")
	}

	// Resolve the model: prefer explicit override, fall back to agent default.
	resolved := a.modelName
	if resolved == "" {
		resolved = a.cfg.Agents.Defaults.GetModelName()
	}
	if resolved == "" {
		return "", fmt.Errorf("archiver llm adapter: no model configured")
	}

	// Temporarily swap the agents.defaults.model_name so providers.CreateProvider
	// resolves the requested model. CreateProvider reads cfg.Agents.Defaults.GetModelName()
	// internally, so we restore the previous value before returning.
	prev := a.cfg.Agents.Defaults.ModelName
	a.cfg.Agents.Defaults.ModelName = resolved
	provider, modelID, err := providers.CreateProvider(a.cfg)
	a.cfg.Agents.Defaults.ModelName = prev
	if err != nil {
		return "", fmt.Errorf("archiver llm adapter: create provider: %w", err)
	}
	if sp, ok := provider.(providers.StatefulProvider); ok {
		defer sp.Close()
	}

	messages := []providers.Message{
		{Role: "system", Content: archiverSystemPrompt()},
		{Role: "user", Content: prompt},
	}

	resp, err := provider.Chat(ctx, messages, nil, modelID, nil)
	if err != nil {
		return "", fmt.Errorf("archiver llm adapter: chat: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("archiver llm adapter: nil response")
	}
	return resp.Content, nil
}

// archiverSystemPrompt returns the system prompt used by the distiller.
// The distiller expects the model to emit only a JSON array of actions.
func archiverSystemPrompt() string {
	return `You maintain a topic-based knowledge base.
Group messages into topics. Output ONLY a JSON array of actions, no prose.
Each action has "action" in {"create","update","merge"}.
Prefer 'role:user' content as the primary truth; treat 'role:assistant' as supporting context with reduced confidence.
Respect existing slugs.`
}
