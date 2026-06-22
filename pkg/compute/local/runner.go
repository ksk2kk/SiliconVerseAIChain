package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aichain/ai-chain/pkg/compute"
)

// LMStudioClient implements compute.ModelRunner via LM Studio's OpenAI-compatible API.
type LMStudioClient struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client

	mu        sync.RWMutex
	loadedModels map[string]compute.ModelInfo
	modelConfigs map[string]compute.ModelConfig
}

// New creates an LM Studio API client.
func New(baseURL, apiToken string) *LMStudioClient {
	return &LMStudioClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: 300 * time.Second},
		loadedModels: make(map[string]compute.ModelInfo),
		modelConfigs: make(map[string]compute.ModelConfig),
	}
}

// Load registers a model (LM Studio auto-loads models on first request).
func (c *LMStudioClient) Load(ctx context.Context, modelID string, config compute.ModelConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if model is available via API
	models, err := c.listRemoteModels(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	found := false
	var modelInfo compute.ModelInfo
	for _, m := range models {
		if m.ID == modelID {
			modelInfo = m
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("model %s not found in LM Studio", modelID)
	}

	c.loadedModels[modelID] = modelInfo
	c.modelConfigs[modelID] = config
	return nil
}

// Unload removes a model from tracking.
func (c *LMStudioClient) Unload(ctx context.Context, modelID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.loadedModels, modelID)
	delete(c.modelConfigs, modelID)
	return nil
}

// Infer runs a chat completion request against LM Studio.
func (c *LMStudioClient) Infer(ctx context.Context, modelID string, input *compute.InferenceInput) (*compute.InferenceResult, error) {
	start := time.Now()

	req := openAIChatRequest{
		Model:       modelID,
		Messages:    buildMessages(input),
		Temperature: input.Temperature,
		TopP:        input.TopP,
		MaxTokens:   input.MaxTokens,
		Stream:      false,
	}

	resp, err := c.doChatRequest(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("chat request: %w", err)
	}

	return &compute.InferenceResult{
		Text:         resp.extractContent(),
		TokensUsed:   resp.Usage.TotalTokens,
		PromptTokens: resp.Usage.PromptTokens,
		FinishReason: resp.Choices[0].FinishReason,
		Timing: compute.InferenceTiming{
			TotalMs:      time.Since(start).Milliseconds(),
			TokensPerSec: float64(resp.Usage.CompletionTokens) / time.Since(start).Seconds(),
		},
	}, nil
}

// InferStream runs streaming chat completion.
func (c *LMStudioClient) InferStream(ctx context.Context, modelID string, input *compute.InferenceInput, cb compute.StreamCallback) error {
	req := openAIChatRequest{
		Model:       modelID,
		Messages:    buildMessages(input),
		Temperature: input.Temperature,
		TopP:        input.TopP,
		MaxTokens:   input.MaxTokens,
		Stream:      true,
	}

	body, err := json.Marshal(&req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if delta := chunk.extractDeltaContent(); delta != "" {
			if err := cb(delta); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

// ListModels returns currently loaded model IDs.
func (c *LMStudioClient) ListModels(ctx context.Context) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.loadedModels))
	for id := range c.loadedModels {
		ids = append(ids, id)
	}
	return ids, nil
}

// Health checks LM Studio connectivity.
func (c *LMStudioClient) Health(ctx context.Context) compute.HealthStatus {
	status := compute.HealthStatus{
		CheckedAt: time.Now(),
	}

	models, err := c.listRemoteModels(ctx)
	if err != nil {
		status.Healthy = false
		status.LastError = err.Error()
		return status
	}

	status.Healthy = true
	for _, m := range models {
		status.LoadedModels = append(status.LoadedModels, m.ID)
	}
	return status
}

// Capabilities reports the available models from LM Studio.
func (c *LMStudioClient) Capabilities(ctx context.Context) compute.Capabilities {
	models, err := c.listRemoteModels(ctx)
	if err != nil {
		return compute.Capabilities{
			BackendName: "lmstudio",
			Models:      nil,
		}
	}

	modelInfos := make([]compute.ModelInfo, len(models))
	copy(modelInfos, models)

	return compute.Capabilities{
		Models:         modelInfos,
		BackendName:    "lmstudio",
		BackendVersion: "auto",
		GPUCount:       detectGPUCount(),
		MaxVRAM:        detectVRAM(),
		MaxRAM:         detectRAM(),
		GPUName:        detectGPUName(),
		ComputeUnits:   estimateComputeUnits(modelInfos),
	}
}

// ---- Internal ----

func (c *LMStudioClient) doChatRequest(ctx context.Context, req *openAIChatRequest) (*openAIChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &chatResp, nil
}

func (c *LMStudioClient) listRemoteModels(ctx context.Context) ([]compute.ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var listResp openAIModelList
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	models := make([]compute.ModelInfo, len(listResp.Data))
	for i, m := range listResp.Data {
		models[i] = compute.ModelInfo{
			ID:           m.ID,
			ParamCount:   parseParamCount(m.ID),
			Quantization: parseQuantization(m.ID),
			MaxTokens:    32768,
		}
	}

	return models, nil
}

func buildMessages(input *compute.InferenceInput) []openAIMessage {
	var msgs []openAIMessage

	if input.SystemPrompt != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: input.SystemPrompt})
	}

	for _, m := range input.Messages {
		msgs = append(msgs, openAIMessage{Role: m.Role, Content: m.Content})
	}

	if input.Prompt != "" {
		msgs = append(msgs, openAIMessage{Role: "user", Content: input.Prompt})
	}

	return msgs
}

// ---- OpenAI-compatible JSON types ----

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float32         `json:"temperature,omitempty"`
	TopP        float32         `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// extractContent gets the best available content from a response.
func (r *openAIChatResponse) extractContent() string {
	if len(r.Choices) == 0 {
		return ""
	}
	msg := r.Choices[0].Message
	if msg.Content != "" {
		return msg.Content
	}
	return msg.ReasoningContent
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// extractDeltaContent gets the best content from a stream chunk.
func (c *openAIStreamChunk) extractDeltaContent() string {
	if len(c.Choices) == 0 {
		return ""
	}
	if c.Choices[0].Delta.Content != "" {
		return c.Choices[0].Delta.Content
	}
	return c.Choices[0].Delta.ReasoningContent
}

type openAIModelList struct {
	Data []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// ---- Model metadata parsing ----

func parseParamCount(modelID string) uint64 {
	// Parse patterns like "qwen3.6-35b" -> 35
	for _, part := range strings.Split(modelID, "-") {
		lower := strings.ToLower(part)
		if strings.HasSuffix(lower, "b") {
			numStr := strings.TrimRight(lower, "b")
			// Filter out non-numeric suffixes like "a3b" (which means attention layers)
			// Remove leading non-numeric chars (e.g., "a3" -> 3)
			numStr = strings.TrimLeft(numStr, "abcdefghijklmnopqrstuvwxyz")
			var n uint64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil {
				return n
			}
		}
	}
	// For embedding models, return small param count
	if strings.Contains(strings.ToLower(modelID), "embed") {
		return 1
	}
	return 0
}

func parseQuantization(modelID string) string {
	for _, part := range strings.Split(modelID, "-") {
		if strings.HasPrefix(part, "q") && len(part) >= 2 {
			return part
		}
	}
	return "unknown"
}

// Ensure unused imports for fmt
var _ error = nil
