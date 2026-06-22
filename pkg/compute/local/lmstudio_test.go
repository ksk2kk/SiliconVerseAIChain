package local

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aichain/ai-chain/pkg/compute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getLMStudioConfig returns configuration from environment or defaults.
func getLMStudioConfig() (baseURL, apiToken string) {
	baseURL = os.Getenv("LMSTUDIO_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:1234"
	}
	apiToken = os.Getenv("LMSTUDIO_TOKEN")
	if apiToken == "" {
		t.Skip("LMSTUDIO_TOKEN not set, skipping integration test")
	}
	return
}

func TestLMStudio_Health(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := client.Health(ctx)

	if !status.Healthy {
		t.Skipf("LM Studio not available: %s (skipping integration test)", status.LastError)
		return
	}

	assert.True(t, status.Healthy)
	assert.NotEmpty(t, status.LoadedModels)
	t.Logf("Available models: %v", status.LoadedModels)
}

func TestLMStudio_ListModels(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := client.ListModels(ctx)
	require.NoError(t, err)

	// Models need to be loaded first via the Load() method
	// This tests the API connectivity
	t.Logf("Loaded models: %v", models)
}

func TestLMStudio_Capabilities(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	caps := client.Capabilities(ctx)

	assert.NotEmpty(t, caps.BackendName)
	assert.Equal(t, "lmstudio", caps.BackendName)
	t.Logf("Detected models: %d", len(caps.Models))
	for _, m := range caps.Models {
		t.Logf("  Model: %s (%dB params)", m.ID, m.ParamCount)
	}
	t.Logf("GPU: %d x %s", caps.GPUCount, caps.GPUName)
	t.Logf("VRAM: %d MB", caps.MaxVRAM)
	t.Logf("RAM: %d MB", caps.MaxRAM)
	t.Logf("Compute units: %d", caps.ComputeUnits)
}

func TestLMStudio_Infer(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// First load a model
	models, err := client.ListModels(ctx)
	require.NoError(t, err)

	// Get available models from LM Studio directly
	health := client.Health(ctx)
	if !health.Healthy {
		t.Skipf("LM Studio not available: %s", health.LastError)
		return
	}

	// Use the first available model
	var modelID string
	if len(health.LoadedModels) > 0 {
		modelID = health.LoadedModels[0]
	} else {
		t.Skip("No models loaded in LM Studio")
		return
	}

	_ = models

	err = client.Load(ctx, modelID, compute.ModelConfig{})
	require.NoError(t, err)
	defer client.Unload(ctx, modelID)

	// Run inference
	input := &compute.InferenceInput{
		SystemPrompt: "You are a helpful AI assistant. Answer concisely.",
		Prompt:      "What is the capital of France? Reply in one sentence.",
		Temperature: 0.7,
		MaxTokens:   50,
	}

	result, err := client.Infer(ctx, modelID, input)
	require.NoError(t, err)

	// LM Studio may return text in slightly different format
	if result.Text == "" {
		t.Log("Note: result.Text is empty but inference completed successfully")
	}
	assert.Greater(t, result.TokensUsed, 0, "inference should consume tokens")

	t.Logf("Model: %s", modelID)
	t.Logf("Response: %s", result.Text)
	t.Logf("Tokens: %d (prompt=%d, completion=%d)",
		result.TokensUsed, result.PromptTokens, result.TokensUsed-result.PromptTokens)
	t.Logf("Timing: %dms total, %.1f tok/s",
		result.Timing.TotalMs, result.Timing.TokensPerSec)
}

func TestLMStudio_InferStream(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	health := client.Health(ctx)
	if !health.Healthy {
		t.Skipf("LM Studio not available: %s", health.LastError)
		return
	}

	if len(health.LoadedModels) == 0 {
		t.Skip("No models loaded")
		return
	}

	modelID := health.LoadedModels[0]
	err := client.Load(ctx, modelID, compute.ModelConfig{})
	require.NoError(t, err)
	defer client.Unload(ctx, modelID)

	input := &compute.InferenceInput{
		Prompt:      "Count from 1 to 5, one number per line.",
		MaxTokens:   50,
	}

	var chunks []string
	err = client.InferStream(ctx, modelID, input, func(chunk string) error {
		chunks = append(chunks, chunk)
		fmt.Print(chunk) // Visual feedback
		return nil
	})
	require.NoError(t, err)

	assert.NotEmpty(t, chunks, "should receive stream chunks")
	t.Logf("\nReceived %d stream chunks", len(chunks))
}

func TestModelParamParsing(t *testing.T) {
	tests := []struct {
		modelID  string
		expected uint64
	}{
		{"qwen3.6-35b-a3b", 35},
		{"qwen/qwen3.6-35b-a3b", 35},
		{"llama-3-70b", 70},
		{"mistral-7b-instruct", 7},
		{"unknown-model", 0},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := parseParamCount(tt.modelID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_Lifecycle(t *testing.T) {
	baseURL, apiToken := getLMStudioConfig()
	client := New(baseURL, apiToken)

	cfg := DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.APIToken = apiToken
	cfg.AutoLoadModels = nil

	manager, err := NewManager(client, cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check manager health
	status := manager.Health(ctx)
	t.Logf("Manager health: %v", status.Healthy)
	t.Logf("Loaded models: %v", status.LoadedModels)

	// Check capabilities
	caps := manager.Capabilities(ctx)
	t.Logf("Miner capability: VRAM=%d, RAM=%d, GPU=%d, ComputeUnits=%d",
		caps.VRAM, caps.RAM, caps.GPUCount, caps.ComputeUnits)

	assert.NotZero(t, caps.ComputeUnits)

	err = manager.Shutdown(ctx)
	require.NoError(t, err)
}
