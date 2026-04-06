package queries

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

func (r *SessionRepo) Create(ctx context.Context, name *string, gameType, farmMode string, accountIDs []int64) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO farm_sessions (name, game_type, farm_mode, account_ids)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		name, gameType, farmMode, accountIDs,
	).Scan(&id)
	return id, err
}

func (r *SessionRepo) Stop(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE farm_sessions SET status = 'stopped', ended_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) IncrementDrops(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE farm_sessions SET drops_count = drops_count + 1 WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) UpdateHours(ctx context.Context, id int64, hours float32) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE farm_sessions SET total_hours = $1 WHERE id = $2`, hours, id)
	return err
}

func (r *SessionRepo) ActiveSessions(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM farm_sessions WHERE status = 'active'`).Scan(&count)
	return count, err
}
