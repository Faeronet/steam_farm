package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MaFile struct {
	SharedSecret   string `json:"shared_secret"`
	SerialNumber   string `json:"serial_number"`
	RevocationCode string `json:"revocation_code"`
	URI            string `json:"uri"`
	ServerTime     int64  `json:"server_time"`
	AccountName    string `json:"account_name"`
	TokenGID       string `json:"token_gid"`
	IdentitySecret string `json:"identity_secret"`
	Secret1        string `json:"secret_1"`
	Status         int    `json:"status"`
	DeviceID       string `json:"device_id"`
	SteamID        int64  `json:"steamid"`

	FullyEnrolled bool   `json:"fully_enrolled"`
	Session       *SDASession `json:"Session"`
}

type SDASession struct {
	SessionID   string `json:"SessionID"`
	SteamLogin  string `json:"SteamLogin"`
	SteamLoginSecure string `json:"SteamLoginSecure"`
	WebCookie   string `json:"WebCookie"`
	OAuthToken  string `json:"OAuthToken"`
	SteamID     int64  `json:"SteamID"`
}

func LoadMaFile(path string) (*MaFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mafile: %w", err)
	}

	var mf MaFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parse mafile: %w", err)
	}

	if mf.SharedSecret == "" {
		return nil, fmt.Errorf("mafile has no shared_secret")
	}

	return &mf, nil
}

func LoadMaFilesFromDir(dir string) ([]*MaFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var files []*MaFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".mafile") {
			continue
		}

		mf, err := LoadMaFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		files = append(files, mf)
	}

	return files, nil
}

func (mf *MaFile) GenerateCode() (string, error) {
	return GenerateSteamTOTP(mf.SharedSecret)
}

func ExportMaFile(mf *MaFile, path string) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mafile: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
