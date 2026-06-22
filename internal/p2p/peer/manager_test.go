package peer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScore_Validity(t *testing.T) {
	s := &Score{}
	assert.Equal(t, 1.0, s.Validity(), "new score should be 1.0")

	s.MessagesSeen = 10
	s.MessagesValid = 8
	s.MessagesInvalid = 2
	assert.InDelta(t, 0.8, s.Validity(), 0.001)
}

func TestScore_ValidityZero(t *testing.T) {
	s := &Score{
		MessagesSeen:    5,
		MessagesInvalid: 5,
	}
	assert.InDelta(t, 0.0, s.Validity(), 0.001)
}

func TestScore_LatencyEMA(t *testing.T) {
	// Test exponential moving average of latency recording
	latencies := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		100 * time.Millisecond,
	}

	ema := time.Duration(0)
	alpha := 0.2

	ema = latencies[0] // first is raw
	for i := 1; i < len(latencies); i++ {
		ema = time.Duration(
			alpha*float64(latencies[i]) +
				(1-alpha)*float64(ema))
	}

	// After EMA: should converge towards 100-200ms range
	assert.True(t, ema > 50*time.Millisecond && ema < 250*time.Millisecond,
		"EMA latency should be in reasonable range, got %v", ema)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 50, cfg.MaxPeers)
	assert.Equal(t, 1*time.Hour, cfg.BanDuration)
	assert.Equal(t, 30*time.Second, cfg.PruneInterval)
}
