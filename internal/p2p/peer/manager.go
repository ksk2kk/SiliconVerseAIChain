package peer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
)

// Score tracks peer behavior metrics.
type Score struct {
	PeerID          peer.ID
	ConnectedSince  time.Time
	MessagesSeen    uint64
	MessagesValid   uint64
	MessagesInvalid uint64
	LastSeen        time.Time
	Latency         time.Duration
	Blacklisted     bool
	BlacklistReason string
}

// Validity returns the fraction of valid messages (0.0 - 1.0).
func (s *Score) Validity() float64 {
	if s.MessagesSeen == 0 {
		return 1.0
	}
	return float64(s.MessagesValid) / float64(s.MessagesSeen)
}

// Manager handles peer lifecycle, scoring, and banning.
type Manager struct {
	host  host.Host
	scores map[peer.ID]*Score
	mu    sync.RWMutex

	// Config
	maxPeers      int
	minScore      float64 // minimum validity to keep connected
	banDuration   time.Duration
	pruneInterval time.Duration

	// Blacklist (peers banned permanently or for duration)
	blacklist map[peer.ID]time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

// Config holds peer manager settings.
type Config struct {
	MaxPeers      int
	MinScore      float64       // Drop peers below this score. Default: 0.5.
	BanDuration   time.Duration // Default: 1 hour.
	PruneInterval time.Duration // Default: 30 seconds.
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		MaxPeers:      50,
		MinScore:      0.5,
		BanDuration:   1 * time.Hour,
		PruneInterval: 30 * time.Second,
	}
}

// NewManager creates a peer manager.
func NewManager(h host.Host, cfg Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		host:          h,
		scores:        make(map[peer.ID]*Score),
		blacklist:     make(map[peer.ID]time.Time),
		maxPeers:      cfg.MaxPeers,
		minScore:      cfg.MinScore,
		banDuration:   cfg.BanDuration,
		pruneInterval: cfg.PruneInterval,
		ctx:           ctx,
		cancel:        cancel,
	}

	// Monitor connection events
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF:    m.onConnected,
		DisconnectedF: m.onDisconnected,
	})

	go m.pruneLoop()

	return m
}

// onConnected is called when a new peer connects.
func (m *Manager) onConnected(n network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reject blacklisted peers
	if _, banned := m.blacklist[pid]; banned {
		go func() {
			time.Sleep(100 * time.Millisecond)
			n.ClosePeer(pid)
		}()
		return
	}

	if score, exists := m.scores[pid]; exists {
		score.ConnectedSince = time.Now()
		score.LastSeen = time.Now()
	} else {
		m.scores[pid] = &Score{
			PeerID:         pid,
			ConnectedSince: time.Now(),
			LastSeen:       time.Now(),
		}
	}
}

// onDisconnected is called when a peer disconnects.
func (m *Manager) onDisconnected(n network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	m.mu.Lock()
	defer m.mu.Unlock()

	if score, ok := m.scores[pid]; ok {
		score.LastSeen = time.Now()
	}
}

// RecordMessage records that a message was received from a peer.
func (m *Manager) RecordMessage(pid peer.ID, valid bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	score, ok := m.scores[pid]
	if !ok {
		score = &Score{
			PeerID:         pid,
			ConnectedSince: time.Now(),
			LastSeen:       time.Now(),
		}
		m.scores[pid] = score
	}

	score.MessagesSeen++
	score.LastSeen = time.Now()
	if valid {
		score.MessagesValid++
	} else {
		score.MessagesInvalid++
	}
}

// RecordLatency records a peer's response latency.
func (m *Manager) RecordLatency(pid peer.ID, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if score, ok := m.scores[pid]; ok {
		// Exponential moving average
		if score.Latency == 0 {
			score.Latency = latency
		} else {
			alpha := 0.2
			score.Latency = time.Duration(
				float64(alpha)*float64(latency) +
					float64(1-alpha)*float64(score.Latency))
		}
	}
}

// Score returns the score for a peer.
func (m *Manager) Score(pid peer.ID) *Score {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scores[pid]
}

// Ban bans a peer for the configured duration.
func (m *Manager) Ban(pid peer.ID, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.blacklist[pid] = time.Now().Add(m.banDuration)

	if score, ok := m.scores[pid]; ok {
		score.Blacklisted = true
		score.BlacklistReason = reason
	}

	// Close connections to this peer
	m.host.Network().ClosePeer(pid)

	fmt.Printf("[PeerMgr] Banned %s: %s\n", pid.ShortString(), reason)
}

// IsBlacklisted checks if a peer is banned.
func (m *Manager) IsBlacklisted(pid peer.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	expiry, ok := m.blacklist[pid]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// ConnectedPeers returns the list of connected peer IDs.
func (m *Manager) ConnectedPeers() []peer.ID {
	return m.host.Network().Peers()
}

// PeerCount returns the number of connected peers.
func (m *Manager) PeerCount() int {
	return len(m.host.Network().Peers())
}

// ProtectPeer marks a peer as protected (won't be pruned).
func (m *Manager) ProtectPeer(pid peer.ID, tag string) {
	m.host.ConnManager().Protect(pid, tag)
}

// UnprotectPeer removes protection from a peer.
func (m *Manager) UnprotectPeer(pid peer.ID, tag string) {
	m.host.ConnManager().Unprotect(pid, tag)
}

// AddPeerToStore adds a peer to the persistent peerstore.
func (m *Manager) AddPeerToStore(pi peer.AddrInfo) {
	m.host.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)
}

// Close shuts down the peer manager.
func (m *Manager) Close() {
	m.cancel()
}

// pruneLoop periodically removes low-quality peers.
func (m *Manager) pruneLoop() {
	ticker := time.NewTicker(m.pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.prune()
		}
	}
}

func (m *Manager) prune() {
	m.mu.Lock()
	defer m.mu.Unlock()

	connectedPeers := m.host.Network().Peers()
	if len(connectedPeers) <= m.maxPeers {
		return
	}

	// Find peers to disconnect (lowest validity score)
	type candidate struct {
		id    peer.ID
		score float64
	}

	var candidates []candidate
	for _, pid := range connectedPeers {
		if m.host.ConnManager().IsProtected(pid, "") {
			continue
		}
		score := 0.0
		if s, ok := m.scores[pid]; ok {
			score = s.Validity()
		}
		if score < m.minScore {
			candidates = append(candidates, candidate{pid, score})
		}
	}

	// Disconnect the worst offenders
	excess := len(connectedPeers) - m.maxPeers
	for i := 0; i < len(candidates) && i < excess; i++ {
		go func(pid peer.ID) {
			m.host.Network().ClosePeer(pid)
		}(candidates[i].id)
	}
}
