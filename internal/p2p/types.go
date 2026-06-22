package p2p

// This file contains shared P2P type definitions and constants.

// ProtocolVersion tracks the current network protocol version.
const ProtocolVersion = "0.1"

// Event types for P2P event subscriptions.
type EventType int

const (
	EventPeerConnected    EventType = iota
	EventPeerDisconnected
	EventBlockReceived
	EventTransactionReceived
	EventVoteReceived
	EventTaskNotification
)

// String returns the event type name.
func (e EventType) String() string {
	switch e {
	case EventPeerConnected:
		return "PeerConnected"
	case EventPeerDisconnected:
		return "PeerDisconnected"
	case EventBlockReceived:
		return "BlockReceived"
	case EventTransactionReceived:
		return "TransactionReceived"
	case EventVoteReceived:
		return "VoteReceived"
	case EventTaskNotification:
		return "TaskNotification"
	default:
		return "Unknown"
	}
}

// Event represents a P2P event.
type Event struct {
	Type EventType
	Peer string // Peer ID string
	Data []byte // Event-specific payload
}
