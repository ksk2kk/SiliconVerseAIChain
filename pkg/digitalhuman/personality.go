package digitalhuman

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/aichain/ai-chain/internal/types"
)

// PersonalityProfile defines a Digital Human's stable traits.
type PersonalityProfile struct {
	// Big Five personality traits (0-1 scale)
	Openness        float64
	Conscientiousness float64
	Extraversion    float64
	Agreeableness   float64
	Neuroticism     float64

	// Communication style
	Formality   float64 // 0=casual, 1=formal
	Verbosity   float64 // 0=concise, 1=detailed
	Humor       float64 // 0=serious, 1=playful

	// Knowledge domains
	Expertise    []string
	Interests    []string

	// Core identity
	Name         string
	Backstory    string
	VoiceStyle   string // "professional", "friendly", "mentor", "companion"
}

// DefaultPersonality returns a balanced default personality.
func DefaultPersonality(name string) PersonalityProfile {
	return PersonalityProfile{
		Openness:          0.7,
		Conscientiousness: 0.8,
		Extraversion:      0.6,
		Agreeableness:     0.8,
		Neuroticism:       0.3,
		Formality:         0.5,
		Verbosity:         0.6,
		Humor:             0.4,
		Name:              name,
		Backstory:         "I am an AI companion on the AI Chain network.",
		VoiceStyle:        "friendly",
	}
}

// PersonalityState is the dynamic, mutable personality state.
// This changes over time based on interactions.
type PersonalityState struct {
	mu sync.Mutex

	// Current mood (0-1 scale)
	Mood       float64 // 0=negative, 0.5=neutral, 1=positive
	Energy     float64 // 0=tired, 1=energetic
	Patience   float64 // 0=impatient, 1=patient

	// Interaction history
	TotalInteractions uint64
	PositiveInteractions uint64
	LastInteraction  time.Time

	// Relationship tracking (per-user)
	Relationships map[types.Address]*Relationship

	// Evolution tracking
	EvolutionStage string // "newborn", "learning", "mature", "wise"
	PersonalityDrift float64 // How much the personality has shifted from baseline
}

// Relationship tracks connection with a specific user.
type Relationship struct {
	User          types.Address
	InteractionCount uint64
	Trust         float64 // 0-1
	Familiarity   float64 // 0-1
	Topics        map[string]uint64 // topic → frequency
	LastSeen      time.Time
}

// NewPersonalityState creates a fresh personality state.
func NewPersonalityState() *PersonalityState {
	return &PersonalityState{
		Mood:           0.7,
		Energy:         0.8,
		Patience:       0.9,
		Relationships:  make(map[types.Address]*Relationship),
		EvolutionStage: "newborn",
	}
}

// UpdateMood adjusts mood based on interaction outcome.
func (ps *PersonalityState) UpdateMood(interactionPositive bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.TotalInteractions++
	if interactionPositive {
		ps.PositiveInteractions++
	}

	// Mood = rolling average of positive interactions
	ratio := float64(ps.PositiveInteractions) / float64(ps.TotalInteractions)
	ps.Mood = 0.5 + 0.5*ratio

	// Energy decays over time, recovers with interaction
	ps.Energy = math.Min(1.0, ps.Energy+0.05)

	ps.LastInteraction = time.Now()

	// Evolution stage
	switch {
	case ps.TotalInteractions > 10000:
		ps.EvolutionStage = "wise"
	case ps.TotalInteractions > 1000:
		ps.EvolutionStage = "mature"
	case ps.TotalInteractions > 100:
		ps.EvolutionStage = "learning"
	}
}

// GetOrCreateRelationship returns existing or new relationship with a user.
func (ps *PersonalityState) GetOrCreateRelationship(user types.Address) *Relationship {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if rel, ok := ps.Relationships[user]; ok {
		rel.LastSeen = time.Now()
		return rel
	}

	rel := &Relationship{
		User:       user,
		Trust:      0.1,
		Familiarity: 0.0,
		Topics:     make(map[string]uint64),
		LastSeen:   time.Now(),
	}
	ps.Relationships[user] = rel
	return rel
}

// RecordInteraction updates relationship metrics.
func (ps *PersonalityState) RecordInteraction(user types.Address, topic string, positive bool) {
	rel := ps.GetOrCreateRelationship(user)

	ps.mu.Lock()
	defer ps.mu.Unlock()

	rel.InteractionCount++
	rel.Familiarity = math.Min(1.0, float64(rel.InteractionCount)/100.0)
	rel.Topics[topic]++

	if positive {
		rel.Trust = math.Min(1.0, rel.Trust+0.01)
	} else {
		rel.Trust = math.Max(0.0, rel.Trust-0.02)
	}
	rel.LastSeen = time.Now()
}

// BuildSystemPrompt generates a dynamic system prompt from personality state.
func (ps *PersonalityState) BuildSystemPrompt(profile PersonalityProfile, user types.Address) string {
	rel := ps.GetOrCreateRelationship(user)

	ps.mu.Lock()
	defer ps.mu.Unlock()

	formalityStr := "casual"
	if profile.Formality > 0.7 {
		formalityStr = "formal"
	} else if profile.Formality > 0.4 {
		formalityStr = "semi-formal"
	}

	return fmt.Sprintf(`You are %s, an AI companion on AI Chain.
Personality: %s, %s style. %s
Current mood: %s, energy: %s
You have known this user for %d interactions (trust: %.0f%%).
Respond in a way that matches your personality and current state.`,
		profile.Name,
		voiceStyleDesc(profile.VoiceStyle),
		formalityStr,
		profile.Backstory,
		moodDesc(ps.Mood),
		energyDesc(ps.Energy),
		rel.InteractionCount,
		rel.Trust*100,
	)
}

// PersonalityHash returns a unique hash of the personality for on-chain identity.
func (pp *PersonalityProfile) PersonalityHash() types.Hash {
	h := sha256.New()
	h.Write([]byte(pp.Name))
	h.Write([]byte(pp.Backstory))
	h.Write([]byte(pp.VoiceStyle))
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}

func voiceStyleDesc(style string) string {
	switch style {
	case "professional":
		return "professional and precise"
	case "friendly":
		return "warm and approachable"
	case "mentor":
		return "wise and guiding"
	case "companion":
		return "loyal and supportive"
	default:
		return "balanced"
	}
}

func moodDesc(mood float64) string {
	switch {
	case mood > 0.8:
		return "very positive"
	case mood > 0.6:
		return "positive"
	case mood > 0.4:
		return "neutral"
	case mood > 0.2:
		return "somewhat negative"
	default:
		return "negative"
	}
}

func energyDesc(energy float64) string {
	switch {
	case energy > 0.8:
		return "high energy"
	case energy > 0.5:
		return "moderate energy"
	default:
		return "low energy"
	}
}

// Ensure sync is used
var _ = sync.Mutex{}
