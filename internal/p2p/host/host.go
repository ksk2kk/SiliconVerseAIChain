package host

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"

	ma "github.com/multiformats/go-multiaddr"
)

// Host wraps a libp2p host with AI Chain-specific configuration.
type Host struct {
	inner host.Host
	cfg   *Config
}

// Config holds host-level libp2p options.
type Config struct {
	ListenAddrs        []ma.Multiaddr
	ExternalAddr       ma.Multiaddr
	PrivateKey         crypto.PrivKey
	MaxPeers           int
	LowWaterPeers      int
	GracePeriod        time.Duration
	EnableRelay        bool
	EnableNatPortMap   bool
	EnableHolePunching bool
}

// DefaultConfig returns production-ready defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxPeers:         100,
		LowWaterPeers:    50,
		GracePeriod:      20 * time.Second,
		EnableNatPortMap: true,
	}
}

// New creates a new libp2p Host.
func New(ctx context.Context, cfg *Config) (*Host, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Generate or use provided key
	privKey := cfg.PrivateKey
	if privKey == nil {
		var err error
		privKey, _, err = crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate host key: %w", err)
		}
	}

	pid, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("derive peer ID: %w", err)
	}

	// Default listen addresses
	listenAddrs := cfg.ListenAddrs
	if len(listenAddrs) == 0 {
		addr0, _ := ma.NewMultiaddr("/ip4/0.0.0.0/tcp/0")
		addr6, _ := ma.NewMultiaddr("/ip6/::/tcp/0")
		listenAddrs = []ma.Multiaddr{addr0, addr6}
	}

	// Connection manager
	cmgr, err := connmgr.NewConnManager(
		cfg.LowWaterPeers,
		cfg.MaxPeers,
		connmgr.WithGracePeriod(cfg.GracePeriod),
	)
	if err != nil {
		return nil, fmt.Errorf("connmgr: %w", err)
	}

	// Build libp2p with defaults (includes TCP, Noise, Yamux, etc.)
	libp2pOpts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.ConnectionManager(cmgr),
	}

	if cfg.EnableNatPortMap {
		libp2pOpts = append(libp2pOpts, libp2p.NATPortMap())
	}

	if cfg.EnableRelay {
		libp2pOpts = append(libp2pOpts, libp2p.EnableRelay())
	}

	if cfg.EnableHolePunching {
		libp2pOpts = append(libp2pOpts, libp2p.EnableHolePunching())
	}

	inner, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	h := &Host{inner: inner, cfg: cfg}

	fmt.Printf("[P2P Host] PeerID: %s\n", pid.String())
	for _, addr := range inner.Addrs() {
		full, _ := ma.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", addr, pid))
		fmt.Printf("[P2P Host] Listen: %s\n", full)
	}

	return h, nil
}

// Inner returns the underlying libp2p host.
func (h *Host) Inner() host.Host {
	return h.inner
}

// ID returns the local peer ID.
func (h *Host) ID() peer.ID {
	return h.inner.ID()
}

// Addrs returns the host's listen addresses.
func (h *Host) Addrs() []ma.Multiaddr {
	return h.inner.Addrs()
}

// FullAddr returns the full /p2p/ID multiaddr for the given transport addr.
func (h *Host) FullAddr(addr ma.Multiaddr) ma.Multiaddr {
	full, err := ma.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", addr, h.ID()))
	if err != nil {
		return addr
	}
	return full
}

// FullAddrs returns all listen addresses with /p2p suffix.
func (h *Host) FullAddrs() []ma.Multiaddr {
	addrs := h.Addrs()
	full := make([]ma.Multiaddr, len(addrs))
	for i, a := range addrs {
		full[i] = h.FullAddr(a)
	}
	return full
}

// Connect connects to a remote peer.
func (h *Host) Connect(ctx context.Context, pi peer.AddrInfo) error {
	return h.inner.Connect(ctx, pi)
}

// SetStreamHandler sets a protocol stream handler.
func (h *Host) SetStreamHandler(proto string, handler network.StreamHandler) {
	h.inner.SetStreamHandler(protocol.ID(proto), handler)
}

// NewStream opens a new stream to a peer.
func (h *Host) NewStream(ctx context.Context, p peer.ID, proto string, extraProtos ...string) (network.Stream, error) {
	protos := make([]protocol.ID, 0, 1+len(extraProtos))
	protos = append(protos, protocol.ID(proto))
	for _, ep := range extraProtos {
		protos = append(protos, protocol.ID(ep))
	}
	return h.inner.NewStream(ctx, p, protos...)
}

// Peerstore returns the host's peerstore.
func (h *Host) Peerstore() peerstore.Peerstore {
	return h.inner.Peerstore()
}

// Network returns the host's network.
func (h *Host) Network() network.Network {
	return h.inner.Network()
}

// ConnectedPeers returns all connected peer IDs.
func (h *Host) ConnectedPeers() []peer.ID {
	return h.inner.Network().Peers()
}

// PeerCount returns the number of connected peers.
func (h *Host) PeerCount() int {
	return len(h.inner.Network().Peers())
}

// Connections returns all active connections.
func (h *Host) Connections() int {
	return len(h.inner.Network().Conns())
}

// Close shuts down the host.
func (h *Host) Close() error {
	return h.inner.Close()
}

// ParseMultiaddr parses a multiaddr string.
func ParseMultiaddr(s string) (ma.Multiaddr, error) {
	return ma.NewMultiaddr(s)
}

// AddrInfoFromString parses a full /p2p/ID multiaddr string into peer.AddrInfo.
func AddrInfoFromString(s string) (*peer.AddrInfo, error) {
	maddr, err := ma.NewMultiaddr(s)
	if err != nil {
		return nil, err
	}
	return peer.AddrInfoFromP2pAddr(maddr)
}
