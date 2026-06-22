package gossip

// This file documents the wire message formats for each topic.

// GossipBlock is the message broadcast on TopicBlocks.
type GossipBlock struct {
	// Raw block bytes (RLP-encoded or similar) for the full block.
	Data []byte `json:"d"`

	// Hash is the block hash for quick lookup.
	Hash string `json:"h"`
}

// GossipTransaction is the message broadcast on TopicTransactions.
type GossipTransaction struct {
	// Raw transaction bytes.
	Data []byte `json:"d"`

	// Hash is the transaction hash.
	Hash string `json:"h"`
}

// GossipVote is the message broadcast on TopicVotes (prevote/precommit).
type GossipVote struct {
	// Type: "prevote" or "precommit".
	Type string `json:"t"`

	// Height is the consensus height.
	Height uint64 `json:"h"`

	// Round is the consensus round.
	Round uint64 `json:"r"`

	// BlockHash is the block being voted on.
	BlockHash string `json:"b"`

	// Validator is the address of the voting validator.
	Validator string `json:"v"`

	// Signature authenticates the vote.
	Signature string `json:"s"`
}

// GossipTaskNotification is broadcast on TopicTasks when task state changes.
type GossipTaskNotification struct {
	TaskID string `json:"id"`
	Status string `json:"st"` // "created", "assigned", "completed", "disputed"
	Miner  string `json:"m"`  // Assigned miner, if any
}
