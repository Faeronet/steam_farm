package service

import (
	"context"

	"github.com/faeronet/steam-farm-system/internal/database/queries"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionService struct {
	repo *queries.SessionRepo
}

func NewSessionService(pool *pgxpool.Pool) *SessionService {
	return &SessionService{
		repo: queries.NewSessionRepo(pool),
	}
}

func (s *SessionService) Create(ctx context.Context, name *string, gameType, farmMode string, accountIDs []int64) (int64, error) {
	return s.repo.Create(ctx, name, gameType, farmMode, accountIDs)
}

func (s *SessionService) Stop(ctx context.Context, id int64) error {
	return s.repo.Stop(ctx, id)
}

func (s *SessionService) IncrementDrops(ctx context.Context, id int64) error {
	return s.repo.IncrementDrops(ctx, id)
}
