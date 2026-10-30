package dlq

import (
	"context"
	"errors"
	"fmt"

	"osyn/internal/database"
)

type Store interface {
	GetDeadLetter(ctx context.Context, id string) (database.DeadLetter, error)
	ReplayDeadLetter(ctx context.Context, id, subject string) (string, error)
	ResolveDeadLetter(ctx context.Context, id string) (database.DeadLetter, error)
	RecordReplayFailure(ctx context.Context, id string, cause error) error
}

type Service struct {
	store           Store
	incomingSubject string
}

func NewService(store Store, incomingSubject string) *Service {
	return &Service{store: store, incomingSubject: incomingSubject}
}

func (s *Service) Get(ctx context.Context, id string) (database.DeadLetter, error) {
	return s.store.GetDeadLetter(ctx, id)
}

func (s *Service) Resolve(ctx context.Context, id string) (database.DeadLetter, error) {
	return s.store.ResolveDeadLetter(ctx, id)
}

func (s *Service) Replay(ctx context.Context, id string) (string, error) {
	eventID, err := s.store.ReplayDeadLetter(ctx, id, s.incomingSubject)
	if err == nil {
		return eventID, nil
	}
	if !errors.Is(err, database.ErrNotFound) && !errors.Is(err, database.ErrDLQResolved) {
		if recordErr := s.store.RecordReplayFailure(ctx, id, err); recordErr != nil {
			return "", fmt.Errorf("%w; record replay failure: %v", err, recordErr)
		}
	}
	return "", err
}
