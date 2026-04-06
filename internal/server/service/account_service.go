package service

import (
	"context"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/faeronet/steam-farm-system/internal/database/queries"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AccountService struct {
	repo          *queries.AccountRepo
	encryptionKey []byte
}

func NewAccountService(pool *pgxpool.Pool, encryptionKey []byte) *AccountService {
	return &AccountService{
		repo:          queries.NewAccountRepo(pool),
		encryptionKey: encryptionKey,
	}
}

func (s *AccountService) List(ctx context.Context, gameType, status string) ([]models.Account, error) {
	return s.repo.List(ctx, gameType, status)
}

func (s *AccountService) Get(ctx context.Context, id int64) (*models.Account, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *AccountService) Create(ctx context.Context, username, password string, sharedSecret, identitySecret *string, gameType, farmMode string) (int64, error) {
	enc, err := common.Encrypt([]byte(password), s.encryptionKey)
	if err != nil {
		return 0, err
	}
	return s.repo.Create(ctx, username, enc, sharedSecret, identitySecret, gameType, farmMode, nil, nil)
}

func (s *AccountService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *AccountService) UpdateStatus(ctx context.Context, id int64, status, detail string) error {
	return s.repo.UpdateStatus(ctx, id, status, detail)
}

func (s *AccountService) DecryptPassword(account *models.Account) (string, error) {
	plain, err := common.Decrypt(account.PasswordEnc, s.encryptionKey)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *AccountService) StatusCounts(ctx context.Context) (map[string]int, error) {
	return s.repo.CountByStatus(ctx)
}
