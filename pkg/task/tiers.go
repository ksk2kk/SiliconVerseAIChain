package task

// Tier requirements define the minimum miner capability for each task level.
// Like Bitcoin's transaction version defining consensus rules.

// TierRequirements describes what a miner needs to handle tasks of a given tier.
type TierRequirements struct {
	MinVRAM      uint64 // Minimum GPU VRAM in MB
	MinRAM       uint64 // Minimum system RAM in MB
	MinGPUCount  int    // Minimum number of GPUs
	MinComputeUnits uint64 // Minimum compute power score
	MaxInputSize uint64 // Maximum input size in bytes
	ModelParams  uint64 // Approximate model parameters (billions)
}

// GetTierRequirements returns requirements for each tier.
func GetTierRequirements(tier TaskTier) TierRequirements {
	switch tier {
	case Tier1_Lightweight:
		return TierRequirements{
			MinVRAM:      1024,  // 1 GB
			MinRAM:       4096,  // 4 GB
			MinGPUCount:  0,     // CPU-only OK
			MinComputeUnits: 1,
			MaxInputSize: 8 * 1024, // 8 KB
			ModelParams:  0,        // < 1B
		}
	case Tier2_Conversation:
		return TierRequirements{
			MinVRAM:      6144,  // 6 GB
			MinRAM:       16384, // 16 GB
			MinGPUCount:  1,
			MinComputeUnits: 10,
			MaxInputSize: 32 * 1024, // 32 KB
			ModelParams:  7,         // ~7B
		}
	case Tier3_Inference:
		return TierRequirements{
			MinVRAM:      24576, // 24 GB
			MinRAM:       65536, // 64 GB
			MinGPUCount:  1,
			MinComputeUnits: 100,
			MaxInputSize: 128 * 1024, // 128 KB
			ModelParams:  70,         // ~70B
		}
	case Tier4_Heavy:
		return TierRequirements{
			MinVRAM:      81920, // 80 GB
			MinRAM:       262144, // 256 GB
			MinGPUCount:  2,
			MinComputeUnits: 1000,
			MaxInputSize: 512 * 1024, // 512 KB
			ModelParams:  200,        // 70B - 200B
		}
	case Tier5_Distributed:
		return TierRequirements{
			MinVRAM:      163840, // 160 GB (across nodes)
			MinRAM:       524288, // 512 GB
			MinGPUCount:  4,
			MinComputeUnits: 10000,
			MaxInputSize: 2 * 1024 * 1024, // 2 MB
			ModelParams:  500,             // 500B+
		}
	default:
		return TierRequirements{}
	}
}

// CanMinerHandle checks if a miner with given capability can handle a task tier.
func CanMinerHandle(tier TaskTier, cap MinerCapability) bool {
	req := GetTierRequirements(tier)
	return cap.VRAM >= req.MinVRAM &&
		cap.RAM >= req.MinRAM &&
		cap.GPUCount >= req.MinGPUCount &&
		cap.ComputeUnits >= req.MinComputeUnits
}

// MinerCapability describes a miner's hardware and software capabilities.
type MinerCapability struct {
	VRAM          uint64   // GPU VRAM in MB
	RAM           uint64   // System RAM in MB
	GPUCount      int      // Number of GPUs
	ComputeUnits  uint64   // Normalized compute power score
	SupportedModels []string // Model IDs this miner supports
	MaxBatchSize  int      // Maximum concurrent task batch
	Reputation    float64  // Historical reliability (0-1)
	Uptime        float64  // Fraction of time online
}

// Score calculates a composite score for miner selection.
// Higher score = better candidate for task dispatch.
func (mc MinerCapability) Score(tier TaskTier) float64 {
	req := GetTierRequirements(tier)
	if req.MinComputeUnits == 0 {
		return 0
	}

	// Capability surplus over requirements
	capScore := float64(mc.ComputeUnits) / float64(req.MinComputeUnits)
	if capScore > 5.0 {
		capScore = 5.0 // Cap at 5x
	}

	// Weight: 40% capability, 30% reputation, 30% uptime
	return 0.4*capScore + 0.3*mc.Reputation + 0.3*mc.Uptime
}
