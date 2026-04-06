package service

import (
	"context"

	"github.com/faeronet/steam-farm-system/internal/database/queries"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FarmService struct {
	sessionRepo *queries.SessionRepo
	accountRepo *queries.AccountRepo
}

func NewFarmService(pool *pgxpool.Pool) *FarmService {
	return &FarmService{
		sessionRepo: queries.NewSessionRepo(pool),
		accountRepo: queries.NewAccountRepo(pool),
	}
}

func (s *FarmService) CreateSession(ctx context.Context, name *string, gameType, farmMode string, accountIDs []int64) (int64, error) {
	id, err := s.sessionRepo.Create(ctx, name, gameType, farmMode, accountIDs)
	if err != nil {
		return 0, err
	}

	for _, accID := range accountIDs {
		_ = s.accountRepo.UpdateStatus(ctx, accID, "queued", "session started")
	}

	return id, nil
}

func (s *FarmService) StopSession(ctx context.Context, id int64) error {
	return s.sessionRepo.Stop(ctx, id)
}

func (s *FarmService) ActiveSessions(ctx context.Context) (int, error) {
	return s.sessionRepo.ActiveSessions(ctx)
}
