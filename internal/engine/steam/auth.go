package steam

import (
	"fmt"

	"github.com/faeronet/steam-farm-system/internal/engine/guard"
)

type AuthProvider struct {
	sharedSecret   string
	identitySecret string
	steamID        string
}

func NewAuthProvider(sharedSecret, identitySecret, steamID string) *AuthProvider {
	return &AuthProvider{
		sharedSecret:   sharedSecret,
		identitySecret: identitySecret,
		steamID:        steamID,
	}
}

func (a *AuthProvider) GetTwoFactorCode() (string, error) {
	if a.sharedSecret == "" {
		return "", fmt.Errorf("no shared_secret configured")
	}
	return guard.GenerateSteamTOTP(a.sharedSecret)
}

func (a *AuthProvider) GetDeviceID() string {
	return guard.DeviceID(a.steamID)
}

func (a *AuthProvider) HasTwoFactor() bool {
	return a.sharedSecret != ""
}

func (a *AuthProvider) HasConfirmation() bool {
	return a.identitySecret != ""
}
