package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aichain/ai-chain/pkg/compute"
	"github.com/aichain/ai-chain/pkg/compute/local"
	"github.com/aichain/ai-chain/pkg/compute/registry"
	"github.com/aichain/ai-chain/pkg/task"

	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/types"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:1234", "LM Studio base URL")
	apiToken := flag.String("token", "", "LM Studio API token")
	modelID := flag.String("model", "", "Model ID to use (empty = first available)")
	flag.Parse()

	if *apiToken == "" {
		*apiToken = os.Getenv("LMSTUDIO_TOKEN")
	}

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║   AI Chain - Compute Integration Test    ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()

	ctx := context.Background()

	// 1. Create LM Studio client
	fmt.Println("[1/6] Connecting to LM Studio...")
	client := local.New(*baseURL, *apiToken)

	status := client.Health(ctx)
	if !status.Healthy {
		log.Fatalf("❌ LM Studio not healthy: %s", status.LastError)
	}
	fmt.Printf("  ✅ Connected. Models: %v\n", status.LoadedModels)

	// 2. Discover capabilities
	fmt.Println("[2/6] Detecting hardware capabilities...")
	caps := client.Capabilities(ctx)

	fmt.Printf("  Backend: %s\n", caps.BackendName)
	fmt.Printf("  Models:  %d available\n", len(caps.Models))
	for _, m := range caps.Models {
		fmt.Printf("    - %s (%dB params)\n", m.ID, m.ParamCount)
	}
	if caps.GPUCount > 0 {
		fmt.Printf("  GPU:     %d x %s (%d MB VRAM)\n", caps.GPUCount, caps.GPUName, caps.MaxVRAM)
	}
	fmt.Printf("  RAM:     %d MB\n", caps.MaxRAM)
	fmt.Printf("  Compute: %d units\n", caps.ComputeUnits)

	// 3. Load model
	fmt.Println("[3/6] Selecting model...")
	selectedModel := *modelID
	if selectedModel == "" && len(caps.Models) > 0 {
		selectedModel = caps.Models[0].ID
	}
	if selectedModel == "" {
		log.Fatal("❌ No models available")
	}
	fmt.Printf("  Selected: %s\n", selectedModel)

	err := client.Load(ctx, selectedModel, compute.ModelConfig{})
	if err != nil {
		log.Fatalf("❌ Load failed: %v", err)
	}
	fmt.Println("  ✅ Model loaded")

	// 4. Run inference
	fmt.Println("[4/6] Running inference...")
	input := &compute.InferenceInput{
		SystemPrompt: "You are a blockchain AI assistant. Be concise and technical.",
		Prompt:       "Explain in one paragraph how a Merkle tree enables efficient verification in blockchains.",
		Temperature:  0.7,
		MaxTokens:    200,
	}

	start := time.Now()
	result, err := client.Infer(ctx, selectedModel, input)
	elapsed := time.Since(start)

	if err != nil {
		log.Fatalf("❌ Inference failed: %v", err)
	}

	fmt.Println("  ✅ Inference completed")
	fmt.Printf("  Response (%d tokens, %.1fs):\n", result.TokensUsed, elapsed.Seconds())
	fmt.Printf("  ┌─────────────────────────────────────────┐\n")
	fmt.Printf("  │ %-39s │\n", truncate(result.Text, 39*3))
	fmt.Printf("  └─────────────────────────────────────────┘\n")

	// 5. Create a task from this inference
	fmt.Println("[5/6] Registering as miner and creating task...")

	// Generate a miner identity
	pub, _, _ := crypto.GenerateKey()
	minerAddr := crypto.PubKeyToAddress(pub)

	// Create miner registry
	reg := registry.NewRegistry()

	minerCap := task.MinerCapability{
		VRAM:            caps.MaxVRAM,
		RAM:             caps.MaxRAM,
		GPUCount:        caps.GPUCount,
		ComputeUnits:    caps.ComputeUnits,
		SupportedModels: []string{selectedModel},
		MaxBatchSize:    2,
		Reputation:      1.0,
		Uptime:          1.0,
	}

	err = reg.Register(minerAddr, minerCap, 0)
	if err != nil {
		log.Fatalf("❌ Registration failed: %v", err)
	}
	fmt.Printf("  ✅ Miner registered: %s\n", minerAddr.Hex()[:12]+"...")

	// Create a task matching this inference
	creatorAddr := minerAddr
	taskBuilder := task.NewTaskBuilder().
		SetCreator(creatorAddr).
		SetTier(task.Tier2_Conversation)
	taskBuilder.ModelSpec = task.ModelSpec{
		ModelID:     selectedModel,
		MaxTokens:   200,
		Temperature: 0.7,
	}
	t := taskBuilder.Build()
	fmt.Printf("  ✅ Task created: %s\n", t.ID().Hex()[:12]+"...")

	// 6. Verify through task system
	fmt.Println("[6/6] Verifying task dispatch...")

	// Create dispatcher
	disp := task.NewDispatcher()
	disp.RegisterMiner(minerAddr, minerCap)

	// Try to dispatch the task
	assigned, err := disp.Dispatch(t)
	if err != nil {
		log.Fatalf("❌ Dispatch failed: %v", err)
	}
	fmt.Printf("  ✅ Task assigned to miner: %s\n", assigned.Hex()[:12]+"...")

	// Simulate task completion
	disp.RecordCompletion(assigned, true)
	reg.RecordCompletion(assigned, types.ZeroAmount, true)

	// Print summary
	fmt.Println()
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("  INTEGRATION TEST COMPLETE ✅")
	fmt.Println("══════════════════════════════════════════")
	fmt.Printf("  Miner:      %s\n", minerAddr.Hex()[:16]+"...")
	fmt.Printf("  Model:      %s\n", selectedModel)
	fmt.Printf("  Task:       %s\n", t.ID().Hex()[:16]+"...")
	fmt.Printf("  Tokens:     %d\n", result.TokensUsed)
	fmt.Printf("  Latency:    %.1f seconds\n", elapsed.Seconds())
	fmt.Printf("  Tokens/sec: %.1f\n", result.Timing.TokensPerSec)
	fmt.Println("══════════════════════════════════════════")

	_ = json.Marshal
}

func truncate(s string, maxLen int) string {
	lines := splitLines(s)
	result := ""
	for _, line := range lines {
		if len(line) > maxLen {
			line = line[:maxLen-3] + "..."
		}
		result += line + "\n"
	}
	// Take first 3 lines
	lines = splitLines(result)
	if len(lines) > 3 {
		return joinLines(lines[:3]) + "..."
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
