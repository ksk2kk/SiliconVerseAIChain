package local

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aichain/ai-chain/pkg/compute"
	"github.com/aichain/ai-chain/pkg/task"
)

// Manager handles model lifecycle and coordinates with the task system.
type Manager struct {
	runner  compute.ModelRunner
	config  Config

	mu       sync.RWMutex
	loaded   map[string]*loadedModel
	taskChan chan *task.Task // Tasks to process
}

// Config holds local compute settings.
type Config struct {
	// LM Studio connection
	BaseURL  string
	APIToken string

	// Model management
	MaxLoadedModels int
	AutoLoadModels  []string // Models to load on startup
	ModelCacheDir   string

	// Task processing
	MaxConcurrentTasks int
	TaskPollInterval   time.Duration
}

// DefaultConfig returns a default local compute configuration.
func DefaultConfig() Config {
	return Config{
		BaseURL:            "http://127.0.0.1:1234",
		MaxLoadedModels:    3,
		MaxConcurrentTasks: 2,
		TaskPollInterval:   5 * time.Second,
	}
}

type loadedModel struct {
	info       compute.ModelInfo
	loadedAt   time.Time
	lastUsedAt time.Time
	taskCount  uint64
}

// NewManager creates a local compute manager.
func NewManager(runner compute.ModelRunner, cfg Config) (*Manager, error) {
	m := &Manager{
		runner:   runner,
		config:   cfg,
		loaded:   make(map[string]*loadedModel),
		taskChan: make(chan *task.Task, 100),
	}

	// Auto-load configured models
	for _, modelID := range cfg.AutoLoadModels {
		if err := m.loadModel(context.Background(), modelID); err != nil {
			return nil, fmt.Errorf("auto-load %s: %w", modelID, err)
		}
	}

	return m, nil
}

// ProcessTask executes an AI task using the local model runner.
func (m *Manager) ProcessTask(ctx context.Context, t *task.Task) (*compute.InferenceResult, error) {
	modelID := selectModelForTask(t)

	// Ensure model is loaded
	if err := m.loadModel(ctx, modelID); err != nil {
		return nil, fmt.Errorf("load model %s: %w", modelID, err)
	}

	// Build inference input from task
	input := &compute.InferenceInput{
		SystemPrompt: buildSystemPrompt(t),
		Temperature:  t.ModelSpec().Temperature,
		TopP:         t.ModelSpec().TopP,
		MaxTokens:    int(t.ModelSpec().MaxTokens),
	}

	// For T1/T2 tasks with text input, set prompt directly
	input.Prompt = "Task input hash: " + t.InputHash().Hex()
	// In production: fetch actual input data from storage

	result, err := m.runner.Infer(ctx, modelID, input)
	if err != nil {
		return nil, fmt.Errorf("inference: %w", err)
	}

	m.mu.Lock()
	if lm, ok := m.loaded[modelID]; ok {
		lm.lastUsedAt = time.Now()
		lm.taskCount++
	}
	m.mu.Unlock()

	return result, nil
}

// Capabilities reports the manager's capabilities in dispatcher format.
func (m *Manager) Capabilities(ctx context.Context) task.MinerCapability {
	caps := m.runner.Capabilities(ctx)

	// Convert compute.Capabilities to task.MinerCapability
	supportedModels := make([]string, len(caps.Models))
	for i, m := range caps.Models {
		supportedModels[i] = m.ID
	}

	return task.MinerCapability{
		VRAM:            caps.MaxVRAM,
		RAM:             caps.MaxRAM,
		GPUCount:        caps.GPUCount,
		ComputeUnits:    caps.ComputeUnits,
		SupportedModels: supportedModels,
		MaxBatchSize:    m.config.MaxConcurrentTasks,
		Reputation:      1.0, // New miner starts with perfect rep
		Uptime:          1.0,
	}
}

// Health reports the manager's health.
func (m *Manager) Health(ctx context.Context) compute.HealthStatus {
	return m.runner.Health(ctx)
}

// Shutdown gracefully stops task processing.
func (m *Manager) Shutdown(ctx context.Context) error {
	close(m.taskChan)
	return nil
}

func (m *Manager) loadModel(ctx context.Context, modelID string) error {
	m.mu.RLock()
	if _, ok := m.loaded[modelID]; ok {
		m.mu.RUnlock()
		return nil // Already loaded
	}
	m.mu.RUnlock()

	if err := m.runner.Load(ctx, modelID, compute.ModelConfig{}); err != nil {
		return err
	}

	m.mu.Lock()
	m.loaded[modelID] = &loadedModel{
		loadedAt:   time.Now(),
		lastUsedAt: time.Now(),
	}
	m.mu.Unlock()

	return nil
}

func selectModelForTask(t *task.Task) string {
	spec := t.ModelSpec()
	if spec.ModelID != "" {
		return spec.ModelID
	}

	// Fallback: select best model based on task tier
	switch t.Tier() {
	case task.Tier1_Lightweight, task.Tier2_Conversation:
		return "qwen3.6-35b-a3b"
	case task.Tier3_Inference, task.Tier4_Heavy:
		return "qwen3.6-35b-a3b"
	default:
		return "qwen3.6-35b-a3b"
	}
}

func buildSystemPrompt(t *task.Task) string {
	switch t.Tier() {
	case task.Tier1_Lightweight:
		return "You are a helpful AI assistant. Provide concise, accurate answers."
	case task.Tier2_Conversation:
		return "You are a conversational AI. Engage naturally and helpfully."
	case task.Tier3_Inference:
		return "You are an analytical AI. Reason step by step and provide thorough analysis."
	default:
		return "You are a helpful AI assistant."
	}
}

// ---- Hardware detection ----

func detectGPUCount() int {
	// Check /sys/class/drm for GPU devices
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "card") && strings.Contains(e.Name(), "render") {
			count++
		}
	}
	if count == 0 {
		count = 1 // Assume at least CPU
	}
	return count
}

func detectGPUName() string {
	// Try nvidia-smi or /proc/driver/nvidia
	if data, err := os.ReadFile("/proc/driver/nvidia/version"); err == nil {
		line := strings.Split(string(data), "\n")[0]
		return strings.TrimSpace(line)
	}
	// Try CPU info fallback
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return "Unknown GPU"
}

func detectVRAM() uint64 {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return detectRAM() / 4
	}

	var totalVRAM uint64
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "card") && !strings.Contains(e.Name(), "render") {
			// Try dedicated VRAM first
			paths := []string{
				fmt.Sprintf("/sys/class/drm/%s/device/mem_info_vram_total", e.Name()),
				fmt.Sprintf("/sys/class/drm/%s/device/mem_info_vis_vram_total", e.Name()),
				fmt.Sprintf("/sys/class/drm/%s/device/mem_info_gtt_total", e.Name()),
			}
			for _, path := range paths {
				if data, err := os.ReadFile(path); err == nil {
					var vram uint64
					fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &vram)
					if vram > totalVRAM {
						totalVRAM = vram
					}
				}
			}
		}
	}

	// Convert bytes to MB if > 1GB (value in bytes, not MB)
	if totalVRAM > 1024*1024 {
		totalVRAM /= 1024 * 1024
	}

	// For APUs and integrated GPUs: use a fraction of system RAM
	if totalVRAM < 1024 {
		sysRAM := detectRAM()
		// AMD APUs can allocate up to 50% of system RAM as VRAM via UMA
		totalVRAM = sysRAM / 2
	}

	if totalVRAM == 0 {
		totalVRAM = detectRAM() / 4
	}
	return totalVRAM
}

func detectRAM() uint64 {
	// /proc/meminfo MemTotal
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return uint64(runtime.GOMAXPROCS(0)) * 1024 // Estimate
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				var kb uint64
				fmt.Sscanf(fields[1], "%d", &kb)
				return kb / 1024 // Convert kB to MB
			}
		}
	}
	return 0
}

func estimateComputeUnits(models []compute.ModelInfo) uint64 {
	// Compute units = sum of param counts * quality factor
	var total uint64
	for _, m := range models {
		total += m.ParamCount * 10 // 10 units per billion params
	}
	if total == 0 {
		total = 100 // Default
	}
	return total
}

// Ensure unused imports compile.
var _ = runtime.Version
