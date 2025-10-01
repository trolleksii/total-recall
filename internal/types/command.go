package types

import "time"

// Command represents a single shell command with metadata
type Command struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type SummarizedCommand struct {
	Command
	Summary string `json:"summary"`
}

// EmbeddedCommand represents a command with its vector embedding
type EmbeddedCommand struct {
	Command
	Vector []float32 `json:"vector"`
	ID     string    `json:"id"`
}

// FeedbackPair represents a user query with selected response for learning
type FeedbackPair struct {
	Query        string    `json:"query"`
	SelectedText string    `json:"selected_text"`
	Timestamp    time.Time `json:"timestamp"`
	QueryVector  []float32 `json:"query_vector"`
	ID           string    `json:"id"`
}

// SearchResult represents a candidate result with metadata
type SearchResult struct {
	Command
	Score         float32 `json:"score"`
	Source        string  `json:"source"` // "command" or "feedback"
	FeedbackBoost float32 `json:"feedback_boost,omitempty"`
}
