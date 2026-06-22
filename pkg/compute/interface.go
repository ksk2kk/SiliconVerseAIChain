package compute

import (
	"context"
	"time"
)

// ModelRunner is the core interface for AI model execution.
// Implementations: local (LM Studio), distributed (tensor-parallel), cloud.
type ModelRunner interface {
	// Load prepares a model for inference.
	Load(ctx context.Context, modelID string, config ModelConfig) error

	// Unload releases a model from memory.
	Unload(ctx context.Context, modelID string) error

	// Infer runs inference on the loaded model.
	Infer(ctx context.Context, modelID string, input *InferenceInput) (*InferenceResult, error)

	// InferStream runs streaming inference, calling the callback for each chunk.
	InferStream(ctx context.Context, modelID string, input *InferenceInput, cb StreamCallback) error

	// ListModels returns all currently loaded models.
	ListModels(ctx context.Context) ([]string, error)

	// Health checks the runner's operational status.
	Health(ctx context.Context) HealthStatus

	// Capabilities reports the runner's hardware and software capabilities.
	Capabilities(ctx context.Context) Capabilities
}

// ModelConfig configures a model for loading.
type ModelConfig struct {
	ModelPath    string // Path or HuggingFace ID
	ContextSize  int    // Max context window in tokens
	GPULayers    int    // Layers offloaded to GPU (-1 = all)
	ThreadCount  int    // CPU threads for inference
	BatchSize    int    // Prompt batch size
	Quantization string // "q4_0", "q8_0", "f16"
}

// InferenceInput is the request for an inference run.
type InferenceInput struct {
	Prompt      string        // Input prompt
	SystemPrompt string       // System prompt / instructions
	Messages    []ChatMessage  // Conversation history
	Temperature float32       // Sampling temperature (0-2)
	TopP        float32       // Nucleus sampling
	TopK        int           // Top-K sampling
	MaxTokens   int           // Max output tokens
	StopTokens  []string      // Stop sequences
}

// ChatMessage is a conversation turn.
type ChatMessage struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// InferenceResult contains the output of an inference run.
type InferenceResult struct {
	Text       string        // Generated text
	TokensUsed int           // Total tokens consumed
	PromptTokens int         // Input tokens
	FinishReason string      // "stop", "length", "error"
	Timing     InferenceTiming
}

// InferenceTiming captures performance metrics.
type InferenceTiming struct {
	PromptMs    int64 // Time to process the prompt
	GenerationMs int64 // Time to generate tokens
	TotalMs     int64 // Total wall clock time
	TokensPerSec float64
}

// StreamCallback receives chunks during streaming inference.
type StreamCallback func(chunk string) error

// HealthStatus reports the runner's operational state.
type HealthStatus struct {
	Healthy    bool
	LoadedModels []string
	MemoryUsedMB uint64
	MemoryFreeMB uint64
	GPUUtilization float64
	LastError  string
	CheckedAt  time.Time
}

// Capabilities describes the runner's hardware and model support.
type Capabilities struct {
	Models       []ModelInfo // Available models
	MaxVRAM      uint64      // MB
	MaxRAM       uint64      // MB
	GPUCount     int
	GPUName      string
	ComputeUnits uint64      // Normalized compute score
	BackendName  string      // "lmstudio", "llamacpp", "vllm"
	BackendVersion string
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID          string // e.g., "qwen3.6-35b-a3b"
	ParamCount  uint64 // Billions of parameters (35)
	Quantization string // "q4_0", "f16"
	MaxTokens   int    // Max context length
}
