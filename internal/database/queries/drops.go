package queries

import (
	"context"
	"encoding/json"

	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DropRepo struct {
	pool *pgxpool.Pool
}

func NewDropRepo(pool *pgxpool.Pool) *DropRepo {
	return &DropRepo{pool: pool}
}

func (r *DropRepo) Create(ctx context.Context, d models.Drop) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO drops (account_id, session_id, game_type, item_name, item_type,
		 item_image_url, asset_id, class_id, instance_id, market_price, choice_options)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		d.AccountID, d.SessionID, d.GameType, d.ItemName, d.ItemType,
		d.ItemImageURL, d.AssetID, d.ClassID, d.InstanceID, d.MarketPrice, d.ChoiceOptions,
	).Scan(&id)
	return id, err
}

func (r *DropRepo) ListByGame(ctx context.Context, gameType string, limit int) ([]models.Drop, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, account_id, session_id, game_type, item_name, item_type,
	          item_image_url, asset_id, class_id, instance_id, context_id, market_price,
	          dropped_at, claimed, sent_to_trade, choice_options, chosen_items
	          FROM drops`
	args := []interface{}{}

	if gameType != "" {
		query += ` WHERE game_type = $1`
		args = append(args, gameType)
		query += ` ORDER BY dropped_at DESC LIMIT $2`
		args = append(args, limit)
	} else {
		query += ` ORDER BY dropped_at DESC LIMIT $1`
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drops []models.Drop
	for rows.Next() {
		var d models.Drop
		if err := rows.Scan(
			&d.ID, &d.AccountID, &d.SessionID, &d.GameType, &d.ItemName, &d.ItemType,
			&d.ItemImageURL, &d.AssetID, &d.ClassID, &d.InstanceID, &d.ContextID, &d.MarketPrice,
			&d.DroppedAt, &d.Claimed, &d.SentToTrade, &d.ChoiceOptions, &d.ChosenItems,
		); err != nil {
			return nil, err
		}
		drops = append(drops, d)
	}
	return drops, nil
}

func (r *DropRepo) ListPending(ctx context.Context) ([]models.Drop, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, account_id, session_id, game_type, item_name, item_type,
		 item_image_url, asset_id, class_id, instance_id, context_id, market_price,
		 dropped_at, claimed, sent_to_trade, choice_options, chosen_items
		 FROM drops WHERE claimed = FALSE AND choice_options IS NOT NULL
		 ORDER BY dropped_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drops []models.Drop
	for rows.Next() {
		var d models.Drop
		if err := rows.Scan(
			&d.ID, &d.AccountID, &d.SessionID, &d.GameType, &d.ItemName, &d.ItemType,
			&d.ItemImageURL, &d.AssetID, &d.ClassID, &d.InstanceID, &d.ContextID, &d.MarketPrice,
			&d.DroppedAt, &d.Claimed, &d.SentToTrade, &d.ChoiceOptions, &d.ChosenItems,
		); err != nil {
			return nil, err
		}
		drops = append(drops, d)
	}
	return drops, nil
}

func (r *DropRepo) Claim(ctx context.Context, dropID int64, chosenItems []string) error {
	chosen, _ := json.Marshal(chosenItems)
	_, err := r.pool.Exec(ctx,
		`UPDATE drops SET claimed = TRUE, chosen_items = $1 WHERE id = $2`,
		chosen, dropID)
	return err
}

func (r *DropRepo) WeeklyCount(ctx context.Context, gameType string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM drops WHERE game_type = $1 AND dropped_at > NOW() - INTERVAL '7 days'`,
		gameType).Scan(&count)
	return count, err
}

func (r *DropRepo) WeeklyValue(ctx context.Context, gameType string) (float32, error) {
	var value float32
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(market_price), 0) FROM drops WHERE game_type = $1 AND dropped_at > NOW() - INTERVAL '7 days'`,
		gameType).Scan(&value)
	return value, err
}
