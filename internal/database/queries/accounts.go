package queries

import (
	"context"

	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AccountRepo struct {
	pool *pgxpool.Pool
}

func NewAccountRepo(pool *pgxpool.Pool) *AccountRepo {
	return &AccountRepo{pool: pool}
}

func (r *AccountRepo) List(ctx context.Context, gameType, status string) ([]models.Account, error) {
	query := `SELECT id, username, password_enc, shared_secret, identity_secret,
	          steam_id, avatar_url, persona_name, proxy, game_type,
	          farm_mode, status, status_detail, is_prime, cs2_level,
	          cs2_xp, cs2_xp_needed, cs2_rank, armory_stars, dota_hours,
	          last_drop_at, farmed_this_week, drop_collected, tags, group_name,
	          created_at, updated_at
	          FROM accounts WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if gameType != "" {
		query += ` AND game_type = $` + itoa(idx)
		args = append(args, gameType)
		idx++
	}
	if status != "" {
		query += ` AND status = $` + itoa(idx)
		args = append(args, status)
		idx++
	}
	query += ` ORDER BY id ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r *AccountRepo) GetByID(ctx context.Context, id int64) (*models.Account, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, username, password_enc, shared_secret, identity_secret,
		 steam_id, avatar_url, persona_name, proxy, game_type,
		 farm_mode, status, status_detail, is_prime, cs2_level,
		 cs2_xp, cs2_xp_needed, cs2_rank, armory_stars, dota_hours,
		 last_drop_at, farmed_this_week, drop_collected, tags, group_name,
		 created_at, updated_at
		 FROM accounts WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	a, err := scanAccount(rows)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AccountRepo) Create(ctx context.Context, username string, passwordEnc []byte, sharedSecret, identitySecret *string, gameType, farmMode string, proxy, groupName *string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO accounts (username, password_enc, shared_secret, identity_secret, game_type, farm_mode, proxy, group_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		username, passwordEnc, sharedSecret, identitySecret, gameType, farmMode, proxy, groupName,
	).Scan(&id)
	return id, err
}

func (r *AccountRepo) UpdateStatus(ctx context.Context, id int64, status string, detail string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET status = $1, status_detail = $2, updated_at = NOW() WHERE id = $3`,
		status, detail, id)
	return err
}

func (r *AccountRepo) UpdateCS2Stats(ctx context.Context, id int64, level, xp, xpNeeded int, rank string, prime bool, stars int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET cs2_level = $1, cs2_xp = $2, cs2_xp_needed = $3, cs2_rank = $4,
		 is_prime = $5, armory_stars = $6, updated_at = NOW() WHERE id = $7`,
		level, xp, xpNeeded, rank, prime, stars, id)
	return err
}

func (r *AccountRepo) UpdateDotaHours(ctx context.Context, id int64, hours float32) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET dota_hours = $1, updated_at = NOW() WHERE id = $2`, hours, id)
	return err
}

func (r *AccountRepo) MarkFarmed(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET farmed_this_week = TRUE, last_drop_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *AccountRepo) MarkCollected(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET drop_collected = TRUE, updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *AccountRepo) ResetWeekly(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE accounts SET farmed_this_week = FALSE, drop_collected = FALSE, updated_at = NOW()`)
	return err
}

func (r *AccountRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	return err
}

func (r *AccountRepo) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM accounts GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		result[status] = count
	}
	return result, nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanAccount(row scannable) (models.Account, error) {
	var a models.Account
	err := row.Scan(
		&a.ID, &a.Username, &a.PasswordEnc, &a.SharedSecret, &a.IdentitySecret,
		&a.SteamID, &a.AvatarURL, &a.PersonaName, &a.Proxy, &a.GameType,
		&a.FarmMode, &a.Status, &a.StatusDetail, &a.IsPrime, &a.CS2Level,
		&a.CS2XP, &a.CS2XPNeeded, &a.CS2Rank, &a.ArmoryStars, &a.DotaHours,
		&a.LastDropAt, &a.FarmedThisWeek, &a.DropCollected, &a.Tags, &a.GroupName,
		&a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

func itoa(i int) string {
	return string(rune('0' + i))
}
