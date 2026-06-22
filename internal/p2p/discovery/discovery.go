package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

// Discovery manages peer discovery for an AI Chain node.
type Discovery struct {
	mdnsService    mdns.Service
	dht            routing.Routing
	bootstrapPeers []peer.AddrInfo
	peerCh         chan peer.AddrInfo
	mu             sync.Mutex
	ctx            context.Context
	stop           context.CancelFunc
}

// Config holds discovery settings.
type Config struct {
	BootstrapPeers []peer.AddrInfo
	EnableMDNS     bool
	MDNSInterval   time.Duration
	EnableDHT      bool
	DHTMode        string // "auto", "lan", "wan"
	DHTBucketSize  int
	RendezVous     string
}

// DefaultConfig returns safe discovery defaults.
func DefaultConfig() Config {
	return Config{
		EnableMDNS:    true,
		MDNSInterval:  30 * time.Second,
		EnableDHT:     true,
		DHTMode:       "auto",
		DHTBucketSize: 20,
		RendezVous:    "aichain/v1",
	}
}

// New creates the Discovery service.
func New(ctx context.Context, h host.Host, cfg Config) (*Discovery, error) {
	ctx, cancel := context.WithCancel(ctx)

	d := &Discovery{
		bootstrapPeers: cfg.BootstrapPeers,
		peerCh:         make(chan peer.AddrInfo, 128),
		ctx:            ctx,
		stop:           cancel,
	}

	if cfg.EnableMDNS {
		if err := d.setupMDNS(h, cfg); err != nil {
			cancel()
			return nil, err
		}
	}

	if cfg.EnableDHT {
		if err := d.setupDHT(ctx, h, cfg); err != nil {
			cancel()
			return nil, err
		}
	}

	go d.bootstrapRoutine(ctx, h)
	go d.handleMDNSPeers(ctx, h)

	return d, nil
}

func (d *Discovery) setupMDNS(h host.Host, cfg Config) error {
	rendezvous := cfg.RendezVous
	if rendezvous == "" {
		rendezvous = "aichain/v1"
	}
	svc := mdns.NewMdnsService(h, rendezvous, d)
	if err := svc.Start(); err != nil {
		return fmt.Errorf("mdns start: %w", err)
	}
	d.mdnsService = svc
	fmt.Printf("[Discovery] mDNS enabled (rendezvous: %s)\n", rendezvous)
	return nil
}

// HandlePeerFound implements mdns.DiscoveryNotifee.
func (d *Discovery) HandlePeerFound(pi peer.AddrInfo) {
	select {
	case d.peerCh <- pi:
	default:
	}
}

func (d *Discovery) setupDHT(ctx context.Context, h host.Host, cfg Config) error {
	bucketSize := cfg.DHTBucketSize
	if bucketSize <= 0 {
		bucketSize = 20
	}
	mode := cfg.DHTMode
	if mode == "" {
		mode = "auto"
	}

	// Use auto mode for DHT (works for both LAN and WAN)
	dhtMode := dht.ModeAuto
	if mode == "wan" {
		dhtMode = dht.ModeServer
	}

	var err error
	d.dht, err = dht.New(ctx, h,
		dht.BucketSize(bucketSize),
		dht.Mode(dhtMode),
	)
	if err != nil {
		return fmt.Errorf("dht creation: %w", err)
	}

	fmt.Printf("[Discovery] DHT enabled (mode: %s)\n", mode)
	return nil
}

func (d *Discovery) bootstrapRoutine(ctx context.Context, h host.Host) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	connected := 0
	for _, pi := range d.bootstrapPeers {
		if pi.ID == h.ID() {
			continue
		}
		if h.Network().Connectedness(pi.ID) == network.Connected {
			connected++
			continue
		}

		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := h.Connect(bctx, pi); err != nil {
			fmt.Printf("[Discovery] bootstrap connect %s: %v\n", pi.ID.ShortString(), err)
		} else {
			connected++
		}
		cancel()
	}

	if d.dht != nil && connected > 0 {
		if bt, ok := d.dht.(*dht.IpfsDHT); ok {
			if err := bt.Bootstrap(ctx); err != nil {
				fmt.Printf("[Discovery] dht bootstrap: %v\n", err)
			}
		}
	}

	fmt.Printf("[Discovery] Bootstrapped: %d peers\n", connected)
}

func (d *Discovery) handleMDNSPeers(ctx context.Context, h host.Host) {
	for {
		select {
		case <-ctx.Done():
			return
		case pi := <-d.peerCh:
			if pi.ID == h.ID() {
				continue
			}
			if h.Network().Connectedness(pi.ID) == network.Connected {
				continue
			}
			bctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := h.Connect(bctx, pi); err != nil {
				fmt.Printf("[Discovery] mdns connect %s: %v\n", pi.ID.ShortString(), err)
			}
			cancel()
		}
	}
}

// PeerChannel returns discovered peers.
func (d *Discovery) PeerChannel() <-chan peer.AddrInfo {
	return d.peerCh
}

// DHT returns the DHT routing table.
func (d *Discovery) DHT() routing.Routing {
	return d.dht
}

// FindPeer looks up a peer via DHT.
func (d *Discovery) FindPeer(ctx context.Context, id peer.ID) (peer.AddrInfo, error) {
	if d.dht == nil {
		return peer.AddrInfo{}, fmt.Errorf("dht not enabled")
	}
	return d.dht.FindPeer(ctx, id)
}

// Close shuts down discovery.
func (d *Discovery) Close() error {
	d.stop()
	if d.mdnsService != nil {
		d.mdnsService.Close()
	}
	if d.dht != nil {
		if ipfsDHT, ok := d.dht.(*dht.IpfsDHT); ok {
			return ipfsDHT.Close()
		}
	}
	return nil
}
