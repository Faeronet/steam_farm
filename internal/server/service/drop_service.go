package service

import (
	"context"

	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/faeronet/steam-farm-system/internal/database/queries"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DropService struct {
	repo *queries.DropRepo
}

func NewDropService(pool *pgxpool.Pool) *DropService {
	return &DropService{
		repo: queries.NewDropRepo(pool),
	}
}

func (s *DropService) RecordDrop(ctx context.Context, drop models.Drop) (int64, error) {
	return s.repo.Create(ctx, drop)
}

func (s *DropService) ListByGame(ctx context.Context, gameType string, limit int) ([]models.Drop, error) {
	return s.repo.ListByGame(ctx, gameType, limit)
}

func (s *DropService) ListPending(ctx context.Context) ([]models.Drop, error) {
	return s.repo.ListPending(ctx)
}

func (s *DropService) Claim(ctx context.Context, dropID int64, choices []string) error {
	return s.repo.Claim(ctx, dropID, choices)
}

func (s *DropService) WeeklyStats(ctx context.Context, gameType string) (int, float32, error) {
	count, err := s.repo.WeeklyCount(ctx, gameType)
	if err != nil {
		return 0, 0, err
	}
	value, err := s.repo.WeeklyValue(ctx, gameType)
	if err != nil {
		return count, 0, err
	}
	return count, value, nil
}
