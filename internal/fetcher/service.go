package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"osyn/internal/africastalking"
	"osyn/internal/database"
)

type Store interface {
	LastReceivedID(ctx context.Context, provider string) (int64, error)
	IngestBatch(ctx context.Context, provider, subject string, messages []database.IncomingMessage) (database.IngestResult, error)
}

type Service struct {
	provider     africastalking.SMSProvider
	store        Store
	providerName string
	subject      string
	interval     time.Duration
	batchSize    int
	logger       *slog.Logger
}

func New(provider africastalking.SMSProvider, store Store, providerName, subject string, interval time.Duration, batchSize int, logger *slog.Logger) *Service {
	if batchSize < 1 {
		batchSize = 1
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Service{
		provider:     provider,
		store:        store,
		providerName: providerName,
		subject:      subject,
		interval:     interval,
		batchSize:    batchSize,
		logger:       logger,
	}
}

func (s *Service) Run(ctx context.Context) error {
	s.poll(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

func (s *Service) poll(ctx context.Context) {
	result, err := s.FetchOnce(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("fetch cycle failed", "error", err)
		}
		return
	}
	if result.Inserted > 0 || result.Duplicates > 0 {
		s.logger.Info("fetched sms messages", "inserted", result.Inserted, "duplicates", result.Duplicates, "last_received_id", result.LastReceivedID)
	}
}

func (s *Service) FetchOnce(ctx context.Context) (database.IngestResult, error) {
	lastReceivedID, err := s.store.LastReceivedID(ctx, s.providerName)
	if err != nil {
		return database.IngestResult{}, err
	}
	messages, err := s.provider.FetchMessages(ctx, strconv.FormatInt(lastReceivedID, 10))
	if err != nil {
		return database.IngestResult{}, err
	}
	if len(messages) == 0 {
		return database.IngestResult{LastReceivedID: lastReceivedID}, nil
	}
	sort.SliceStable(messages, func(i, j int) bool {
		left, leftErr := strconv.ParseInt(messages[i].ID, 10, 64)
		right, rightErr := strconv.ParseInt(messages[j].ID, 10, 64)
		if leftErr != nil || rightErr != nil {
			return false
		}
		return left < right
	})

	var result database.IngestResult
	for start := 0; start < len(messages); start += s.batchSize {
		end := start + s.batchSize
		if end > len(messages) {
			end = len(messages)
		}
		chunk := messages[start:end]
		incoming := make([]database.IncomingMessage, 0, len(chunk))
		for _, message := range chunk {
			id, err := strconv.ParseInt(message.ID, 10, 64)
			if err != nil {
				return result, fmt.Errorf("invalid provider message id %q: %w", message.ID, err)
			}
			if id <= 0 {
				return result, fmt.Errorf("invalid provider message id %q", message.ID)
			}
			incoming = append(incoming, database.IncomingMessage{
				ProviderMessageID: message.ID,
				Sender:            message.From,
				Recipient:         message.To,
				Message:           message.Text,
				ReceivedAt:        message.Date,
			})
		}
		chunkResult, err := s.store.IngestBatch(ctx, s.providerName, s.subject, incoming)
		if err != nil {
			return result, err
		}
		result.Inserted += chunkResult.Inserted
		result.Duplicates += chunkResult.Duplicates
		result.LastReceivedID = chunkResult.LastReceivedID
	}
	return result, nil
}
