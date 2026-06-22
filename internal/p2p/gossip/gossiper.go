package gossip

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
)

// Topic names (versioned for protocol evolution).
const (
	TopicTransactions = "aichain/tx/0.1"
	TopicBlocks       = "aichain/block/0.1"
	TopicVotes        = "aichain/vote/0.1"
	TopicTasks        = "aichain/task/0.1"
)

// AllTopics returns all topic names.
func AllTopics() []string {
	return []string{TopicTransactions, TopicBlocks, TopicVotes, TopicTasks}
}

// Message represents a gossip message on any topic.
type Message struct {
	From       peer.ID
	Topic      string
	Data       []byte
	SeqNo      uint64
	ReceivedAt time.Time
}

// MessageHandler is called when a message is received on a subscribed topic.
type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *Message) error
}

// MessageHandlerFunc adapts a function to MessageHandler.
type MessageHandlerFunc func(ctx context.Context, msg *Message) error

func (f MessageHandlerFunc) HandleMessage(ctx context.Context, msg *Message) error {
	return f(ctx, msg)
}

// PubSub wraps a libp2p GossipSub router.
type PubSub struct {
	inner    *pubsub.PubSub
	host     host.Host
	topics   map[string]*pubsub.Topic
	subs     map[string]*pubsub.Subscription
	handlers map[string]MessageHandler
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// Config holds GossipSub configuration.
type Config struct {
	HeartbeatInterval  time.Duration
	SeenTTL            time.Duration
	MaxMessageSize     int
	ValidateQueueSize  int
	EnablePeerExchange bool
	DirectPeers        []peer.AddrInfo
}

// DefaultConfig returns a safe GossipSub configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:  1 * time.Second,
		SeenTTL:            5 * time.Minute,
		MaxMessageSize:     1 << 20,
		ValidateQueueSize:  256,
		EnablePeerExchange: true,
	}
}

// NewPubSub creates a new GossipSub router.
func NewPubSub(ctx context.Context, h host.Host, cfg Config) (*PubSub, error) {
	ctx, cancel := context.WithCancel(ctx)

	var opts []pubsub.Option
	opts = append(opts,
		pubsub.WithPeerExchange(cfg.EnablePeerExchange),
		pubsub.WithMaxMessageSize(cfg.MaxMessageSize),
	)

	gs, err := pubsub.NewGossipSub(ctx, h, opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gossipsub: %w", err)
	}

	ps := &PubSub{
		inner:    gs,
		host:     h,
		topics:   make(map[string]*pubsub.Topic),
		subs:     make(map[string]*pubsub.Subscription),
		handlers: make(map[string]MessageHandler),
		ctx:      ctx,
		cancel:   cancel,
	}

	return ps, nil
}

// Join joins a topic and subscribes to messages.
func (ps *PubSub) Join(topic string, handler MessageHandler) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.topics[topic]; exists {
		return nil
	}

	t, err := ps.inner.Join(topic)
	if err != nil {
		return fmt.Errorf("join topic %s: %w", topic, err)
	}
	ps.topics[topic] = t

	sub, err := t.Subscribe()
	if err != nil {
		return fmt.Errorf("subscribe topic %s: %w", topic, err)
	}
	ps.subs[topic] = sub

	if handler != nil {
		ps.handlers[topic] = handler
		go ps.consumeLoop(topic, sub, handler)
	}

	return nil
}

// Leave leaves a topic.
func (ps *PubSub) Leave(topic string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if sub, ok := ps.subs[topic]; ok {
		sub.Cancel()
		delete(ps.subs, topic)
	}
	if t, ok := ps.topics[topic]; ok {
		t.Close()
		delete(ps.topics, topic)
	}
	delete(ps.handlers, topic)
	return nil
}

// Publish sends raw data to a topic.
func (ps *PubSub) Publish(topic string, data []byte) error {
	ps.mu.RLock()
	t, ok := ps.topics[topic]
	ps.mu.RUnlock()

	if !ok {
		return fmt.Errorf("not joined to topic %s", topic)
	}
	return t.Publish(ps.ctx, data)
}

// PublishJSON marshals and publishes data as JSON.
func (ps *PubSub) PublishJSON(topic string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	return ps.Publish(topic, data)
}

// TopicPeers returns peers on a topic.
func (ps *PubSub) TopicPeers(topic string) []peer.ID {
	return ps.inner.ListPeers(topic)
}

// Close shuts down the GossipSub router.
func (ps *PubSub) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for _, sub := range ps.subs {
		sub.Cancel()
	}
	for _, t := range ps.topics {
		t.Close()
	}
	ps.cancel()
	return nil
}

// consumeLoop reads messages from a subscription and delivers to the handler.
func (ps *PubSub) consumeLoop(topic string, sub *pubsub.Subscription, handler MessageHandler) {
	for {
		msg, err := sub.Next(ps.ctx)
		if err != nil {
			return
		}

		gm := &Message{
			From:       msg.ReceivedFrom,
			Topic:      topic,
			Data:       msg.Data,
			SeqNo:      0, // Seqno is []byte in newer pubsub
			ReceivedAt: time.Now(),
		}

		go func(m *Message) {
			ctx, cancel := context.WithTimeout(ps.ctx, 5*time.Second)
			defer cancel()
			if err := handler.HandleMessage(ctx, m); err != nil {
				fmt.Printf("[GossipSub] handler error on %s: %v\n", topic, err)
			}
		}(gm)
	}
}

// Ensure pb import is used for documentation.
var _ = (*pb.Message)(nil)
