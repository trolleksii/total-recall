package qdrant

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"

	"total-recall/internal/config"
	"total-recall/internal/types"
)

type Client struct {
	client *qdrant.Client
}

// NewClient creates a new Qdrant client
func NewClient() (*Client, error) {
	cfg := config.Get()
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: cfg.Qdrant.Host,
		Port: cfg.Qdrant.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Qdrant client: %w", err)
	}

	return &Client{client: client}, nil
}

// CreateCollection creates a new collection for storing command embeddings
func (c *Client) CreateCollection(ctx context.Context) error {
	cfg := config.Get()
	err := c.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: cfg.Qdrant.CommandCollection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(cfg.Qdrant.VectorDimensions),
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}
	return nil
}

// CreateFeedbackCollection creates a collection for storing feedback pairs
func (c *Client) CreateFeedbackCollection(ctx context.Context) error {
	cfg := config.Get()
	err := c.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: "feedback_" + cfg.Qdrant.CommandCollection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(cfg.Qdrant.VectorDimensions),
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create feedback collection: %w", err)
	}
	return nil
}

// StoreEmbedding stores a command with its embedding in Qdrant
func (c *Client) StoreEmbeddings(ctx context.Context, cmds []types.EmbeddedCommand) error {
	points := make([]*qdrant.PointStruct, len(cmds))
	for i, cmd := range cmds {
		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewID(cmd.ID),
			Vectors: qdrant.NewVectors(cmd.Vector...),
			Payload: qdrant.NewValueMap(map[string]any{
				"text":      cmd.Text,
				"timestamp": cmd.Timestamp.Unix(),
			}),
		}
	}
	cfg := config.Get()
	_, err := c.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: cfg.Qdrant.CommandCollection,
		Points:         points,
	})
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}
	return nil
}

// SearchSimilar performs a vector similarity search
func (c *Client) SearchSimilar(ctx context.Context, vector []float32, limit int) ([]types.EmbeddedCommand, error) {
	cfg := config.Get()
	limit64 := uint64(limit)
	searchResult, err := c.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: cfg.Qdrant.CommandCollection,
		Query:          qdrant.NewQuery(vector...),
		Limit:          &limit64,
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search similar vectors: %w", err)
	}

	var results []types.EmbeddedCommand
	for _, point := range searchResult {
		payload := point.GetPayload()

		textValue, exists := payload["text"]
		if !exists {
			continue
		}
		text := textValue.GetStringValue()

		timestampValue, exists := payload["timestamp"]
		if !exists {
			continue
		}
		timestamp := timestampValue.GetIntegerValue()

		result := types.EmbeddedCommand{
			Command: types.Command{
				Text:      text,
				Timestamp: timestampFromUnix(timestamp),
			},
			ID: point.GetId().GetUuid(),
		}
		results = append(results, result)
	}

	return results, nil
}

// NewFeedbackID generates a new UUID for feedback pairs
func NewFeedbackID() string {
	return uuid.New().String()
}

// timestampFromUnix converts Unix timestamp to time.Time
func timestampFromUnix(unix int64) time.Time {
	return time.Unix(unix, 0)
}
