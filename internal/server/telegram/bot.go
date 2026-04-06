package telegram

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

type Bot struct {
	token  string
	chatID string
}

func NewBot(token, chatID string) *Bot {
	return &Bot{token: token, chatID: chatID}
}

func (b *Bot) IsConfigured() bool {
	return b.token != "" && b.chatID != ""
}

func (b *Bot) SendMessage(text string) error {
	if !b.IsConfigured() {
		return nil
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id":    {b.chatID},
		"text":       {text},
		"parse_mode": {"HTML"},
	})
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram API returned %d", resp.StatusCode)
	}

	return nil
}

func (b *Bot) NotifyDrop(username, itemName, gameType string) {
	emoji := "🎁"
	if gameType == "dota2" {
		emoji = "⚔️"
	}
	msg := fmt.Sprintf("%s <b>Drop!</b>\n👤 %s\n📦 %s", emoji, username, itemName)
	if err := b.SendMessage(msg); err != nil {
		log.Printf("[Telegram] Send failed: %v", err)
	}
}

func (b *Bot) NotifyError(username, detail string) {
	msg := fmt.Sprintf("🔴 <b>Error</b>\n👤 %s\n❌ %s", username, detail)
	if err := b.SendMessage(msg); err != nil {
		log.Printf("[Telegram] Send failed: %v", err)
	}
}

func (b *Bot) NotifyReward(username, rewardType string) {
	msg := fmt.Sprintf("🎉 <b>Reward Available!</b>\n👤 %s\n🏆 %s", username, rewardType)
	if err := b.SendMessage(msg); err != nil {
		log.Printf("[Telegram] Send failed: %v", err)
	}
}
