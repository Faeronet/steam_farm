package common

import (
	"os"
	"strconv"
)

type ServerConfig struct {
	DatabaseURL string
	HTTPPort    int
	GRPCPort    int
	TelegramToken string
	TelegramChatID string
	EncryptionKey  []byte
}

type ClientConfig struct {
	ServerAddr  string
	GameType    string // "cs2" or "dota2"
	FarmMode    string // "protocol" or "sandbox"
	TradeLink   string
	MaxSandboxes int
}

func LoadServerConfig() *ServerConfig {
	return &ServerConfig{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://sfarm:sfarm_dev_pass@127.0.0.1:5434/steam_farm?sslmode=disable"),
		HTTPPort:       getEnvInt("HTTP_PORT", 8080),
		GRPCPort:       getEnvInt("GRPC_PORT", 9090),
		TelegramToken:  getEnv("TELEGRAM_TOKEN", ""),
		TelegramChatID: getEnv("TELEGRAM_CHAT_ID", ""),
		EncryptionKey:  []byte(getEnv("ENCRYPTION_KEY", "steam-farm-default-key-change-me!")),
	}
}

func LoadClientConfig() *ClientConfig {
	return &ClientConfig{
		ServerAddr:   getEnv("SERVER_ADDR", "localhost:9090"),
		GameType:     getEnv("GAME_TYPE", "cs2"),
		FarmMode:     getEnv("FARM_MODE", "sandbox"),
		TradeLink:    getEnv("TRADE_LINK", ""),
		MaxSandboxes: getEnvInt("MAX_SANDBOXES", 10),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
