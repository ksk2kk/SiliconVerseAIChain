package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/aichain/ai-chain/internal/p2p/gossip"
	"github.com/aichain/ai-chain/internal/p2p/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTwoNodeGossip verifies that two nodes can create hosts,
// connect, publish a message, and receive it via GossipSub.
func TestTwoNodeGossip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Node 1
	h1Cfg := host.DefaultConfig()
	h1, err := host.New(ctx, h1Cfg)
	require.NoError(t, err)
	defer h1.Close()

	// Node 2
	h2Cfg := host.DefaultConfig()
	h2, err := host.New(ctx, h2Cfg)
	require.NoError(t, err)
	defer h2.Close()

	// Connect Node 1 -> Node 2
	err = h1.Connect(ctx, peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	require.NoError(t, err, "nodes should connect")
	t.Logf("Node1 (%s) connected to Node2 (%s)", h1.ID().ShortString(), h2.ID().ShortString())

	// Create GossipSub on Node 2
	gs2Cfg := gossip.DefaultConfig()
	gs2, err := gossip.NewPubSub(ctx, h2.Inner(), gs2Cfg)
	require.NoError(t, err)
	defer gs2.Close()

	// Subscribe Node 2 to test topic
	received := make(chan []byte, 1)
	handler := gossip.MessageHandlerFunc(func(ctx context.Context, msg *gossip.Message) error {
		select {
		case received <- msg.Data:
		default:
		}
		return nil
	})
	err = gs2.Join("aichain/test/0.1", handler)
	require.NoError(t, err)

	// Create GossipSub on Node 1
	gs1Cfg := gossip.DefaultConfig()
	gs1, err := gossip.NewPubSub(ctx, h1.Inner(), gs1Cfg)
	require.NoError(t, err)
	defer gs1.Close()

	err = gs1.Join("aichain/test/0.1", nil)
	require.NoError(t, err)

	// Wait for mesh to form
	time.Sleep(2 * time.Second)

	// Publish from Node 1
	testData := []byte("hello from node1")
	err = gs1.Publish("aichain/test/0.1", testData)
	require.NoError(t, err)
	t.Logf("Node1 published: %s", testData)

	// Wait for Node 2 to receive
	select {
	case data := <-received:
		assert.Equal(t, testData, data, "Node2 should receive the exact message")
		t.Logf("Node2 received: %s", data)
	case <-time.After(10 * time.Second):
		t.Fatal("Node2 did not receive message within timeout")
	}
}

// TestHostCreation verifies that a host can be created with default config.
func TestHostCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := host.New(ctx, host.DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, h)

	assert.NotEmpty(t, h.ID())
	assert.NotEmpty(t, h.Addrs())
	assert.Equal(t, 0, h.PeerCount())

	t.Logf("Created host: %s", h.ID().ShortString())

	err = h.Close()
	require.NoError(t, err)
}

// TestHostConnectivity verifies two hosts can connect to each other.
func TestHostConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	h1, _ := host.New(ctx, nil)
	defer h1.Close()
	h2, _ := host.New(ctx, nil)
	defer h2.Close()

	// Connect h1 -> h2
	err := h1.Connect(ctx, peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	require.NoError(t, err)

	// Verify connectivity
	assert.Equal(t, 1, h1.PeerCount(), "h1 should have 1 peer")
	assert.Equal(t, 1, h2.PeerCount(), "h2 should have 1 peer")
}
