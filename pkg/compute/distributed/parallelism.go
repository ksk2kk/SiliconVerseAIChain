package distributed

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ---- Pipeline + Tensor Parallelism ----
//
// Google GPipe / Megatron-LM hybrid parallelism:
//
//   Pipeline Parallelism: Each miner handles a contiguous range of layers.
//     Miner 0: Layers 0-9, Miner 1: Layers 10-19, etc.
//     Micro-batches are pipelined to keep all miners busy.
//
//   Tensor Parallelism: Each miner handles a shard of each weight matrix.
//     FFN W1: column-wise split across miners
//     FFN W2: row-wise split across miners
//     Attention: head-wise split across miners
//     Requires All-Reduce after each tensor-parallel layer.

// ParallelismConfig configures the parallelism strategy.
type ParallelismConfig struct {
	PipelineSize int // Number of pipeline stages (layers per stage = total_layers / pipeline_size)
	TensorSize   int // Number of tensor-parallel workers per pipeline stage
	NumMicroBatches int // Micro-batches for pipeline scheduling (GPipe style)
}

// DefaultParallelismConfig creates a config based on available miners.
func DefaultParallelismConfig(numMiners int) ParallelismConfig {
	// Auto-tune: try to balance pipeline and tensor parallelism
	// For 8 miners: (PP=4, TP=2) or (PP=2, TP=4)
	switch {
	case numMiners <= 2:
		return ParallelismConfig{PipelineSize: numMiners, TensorSize: 1, NumMicroBatches: 4}
	case numMiners <= 4:
		return ParallelismConfig{PipelineSize: numMiners / 2, TensorSize: 2, NumMicroBatches: 8}
	case numMiners <= 8:
		return ParallelismConfig{PipelineSize: 4, TensorSize: numMiners / 4, NumMicroBatches: 16}
	default:
		return ParallelismConfig{PipelineSize: 8, TensorSize: numMiners / 8, NumMicroBatches: 32}
	}
}

// PipeStage represents one pipeline stage (a contiguous set of layers).
type PipeStage struct {
	StageIndex  int
	FirstLayer  int
	LastLayer   int
	WorkerCount int // Number of tensor-parallel workers
}

// PipelineSchedule is the GPipe-style micro-batch schedule.
type PipelineSchedule struct {
	Stages      []PipeStage
	NumMicroBatches int
	Bubbles     int // Number of pipeline bubble slots (idle time)
}

// BuildPipelineSchedule creates a GPipe-style pipeline schedule.
func BuildPipelineSchedule(cfg ParallelismConfig, numLayers int) (*PipelineSchedule, error) {
	if cfg.PipelineSize <= 0 || cfg.TensorSize <= 0 {
		return nil, fmt.Errorf("invalid parallelism config: PP=%d, TP=%d", cfg.PipelineSize, cfg.TensorSize)
	}

	stages := make([]PipeStage, cfg.PipelineSize)
	layersPerStage := numLayers / cfg.PipelineSize
	remainder := numLayers % cfg.PipelineSize

	layerStart := 0
	for i := 0; i < cfg.PipelineSize; i++ {
		myLayers := layersPerStage
		if i < remainder {
			myLayers++
		}
		stages[i] = PipeStage{
			StageIndex:  i,
			FirstLayer:  layerStart,
			LastLayer:   layerStart + myLayers - 1,
			WorkerCount: cfg.TensorSize,
		}
		layerStart += myLayers
	}

	// GPipe bubble analysis:
	// Warmup:  (P-1) * microbatches
	// Steady:   M * (P) steps
	// Cooldown: (P-1) * microbatches
	// Bubbles per micro-batch: P-1 forward + P-1 backward
	bubbles := (cfg.PipelineSize - 1) * 2

	return &PipelineSchedule{
		Stages:          stages,
		NumMicroBatches: cfg.NumMicroBatches,
		Bubbles:         bubbles,
	}, nil
}

// BubbleFraction returns the fraction of idle time due to pipeline bubbles.
func (ps *PipelineSchedule) BubbleFraction() float64 {
	if ps.NumMicroBatches == 0 {
		return 1.0
	}
	// Bubble fraction ≈ (P-1) / M where P = pipeline stages, M = microbatches
	return float64(len(ps.Stages)-1) / float64(ps.NumMicroBatches)
}

// EstimatedSpeedup estimates the speedup from pipeline + tensor parallelism.
func EstimatedSpeedup(pipelineSize, tensorSize int) float64 {
	// Pipeline efficiency: 1 / (1 + (P-1)/M) where M = microbatches
	// Tensor parallelism: near-linear for small N, sub-linear for large N
	pipeEfficiency := 1.0
	if pipelineSize > 1 {
		pipeEfficiency = 0.85 // Typical GPipe efficiency with enough microbatches
	}
	tensorEfficiency := 1.0
	if tensorSize > 1 {
		// Communication overhead grows with tensor parallel size
		tensorEfficiency = 0.90 - 0.02*float64(tensorSize-2)
		if tensorEfficiency < 0.5 {
			tensorEfficiency = 0.5
		}
	}
	return float64(pipelineSize) * pipeEfficiency * float64(tensorSize) * tensorEfficiency
}

// ---- Pipeline Executor ----

// PipelineExecutor runs the GPipe schedule.
type PipelineExecutor struct {
	schedule *PipelineSchedule
	shards   []ShardAssignment

	// Per-stage queues for micro-batch passing
	stageQueues []chan *microBatch
}

type microBatch struct {
	Index    int
	Data     *Tensor
	Result   *Tensor
	Err      error
	Done     chan struct{}
}

// NewPipelineExecutor creates a pipeline executor.
func NewPipelineExecutor(schedule *PipelineSchedule, shards []ShardAssignment) *PipelineExecutor {
	queues := make([]chan *microBatch, len(schedule.Stages)+1)
	for i := range queues {
		queues[i] = make(chan *microBatch, schedule.NumMicroBatches)
	}

	return &PipelineExecutor{
		schedule:    schedule,
		shards:      shards,
		stageQueues: queues,
	}
}

// Execute runs the pipeline with micro-batch scheduling.
func (pe *PipelineExecutor) Execute(ctx context.Context, input *Tensor) ([]*Tensor, error) {
	stages := pe.schedule.Stages
	M := pe.schedule.NumMicroBatches

	// Split input into micro-batches
	results := make([]*Tensor, M)
	var wg sync.WaitGroup

	// Start pipeline stages
	for s, stage := range stages {
		wg.Add(1)
		go func(sIdx int, stg PipeStage) {
			defer wg.Done()
			pe.runStage(ctx, sIdx, stg)
		}(s, stage)
	}

	// Feed micro-batches
	for m := 0; m < M; m++ {
		mb := &microBatch{
			Index: m,
			Data:  input, // In production: split input into micro-batches
			Done:  make(chan struct{}),
		}
		pe.stageQueues[0] <- mb
	}

	// Collect results from last stage
	for m := 0; m < M; m++ {
		select {
		case mb := <-pe.stageQueues[len(stages)]:
			results[mb.Index] = mb.Result
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	wg.Wait()
	return results, nil
}

func (pe *PipelineExecutor) runStage(ctx context.Context, stageIdx int, stage PipeStage) {
	inQueue := pe.stageQueues[stageIdx]
	outQueue := pe.stageQueues[stageIdx+1]

	for {
		select {
		case <-ctx.Done():
			return
		case mb := <-inQueue:
			// Process micro-batch through this pipeline stage
			// In production: send to actual miner, receive output
			result := pe.simulateStageForward(mb.Data, stage)
			mb.Result = result
			select {
			case outQueue <- mb:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (pe *PipelineExecutor) simulateStageForward(input *Tensor, stage PipeStage) *Tensor {
	// Simulate computation through layers in this stage
	output := NewTensor(input.Shape...)
	copy(output.Data, input.Data)
	for i := range output.Data {
		output.Data[i] *= 1.0001 // Simulated computation
	}
	// Simulated compute time proportional to layers
	numLayers := stage.LastLayer - stage.FirstLayer + 1
	time.Sleep(time.Duration(numLayers) * time.Microsecond)
	return output
}

// Ensure imports used.
var _ = sync.WaitGroup{}
