package guard

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

type ConfirmationType int

const (
	ConfirmationGeneric       ConfirmationType = 1
	ConfirmationTrade         ConfirmationType = 2
	ConfirmationMarketListing ConfirmationType = 3
)

func GenerateConfirmationKey(identitySecret string, t time.Time, tag string) (string, error) {
	secret, err := base64.StdEncoding.DecodeString(identitySecret)
	if err != nil {
		return "", fmt.Errorf("decode identity_secret: %w", err)
	}

	bufLen := 8 + len(tag)
	if bufLen > 8+32 {
		bufLen = 8 + 32
	}
	buf := make([]byte, bufLen)
	binary.BigEndian.PutUint64(buf, uint64(t.Unix()))

	tagLen := len(tag)
	if tagLen > 32 {
		tagLen = 32
	}
	copy(buf[8:], tag[:tagLen])

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)

	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func GenerateConfirmationQueryParams(identitySecret string, tag string) (map[string]string, error) {
	now := time.Now()
	key, err := GenerateConfirmationKey(identitySecret, now, tag)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"p":   DeviceID(""), // placeholder, needs steamID
		"a":   "",           // steamID64
		"k":   key,
		"t":   fmt.Sprintf("%d", now.Unix()),
		"m":   "android",
		"tag": tag,
	}, nil
}

func DeviceID(steamID string) string {
	mac := hmac.New(sha1.New, []byte(steamID))
	mac.Write([]byte(steamID))
	hash := mac.Sum(nil)
	return fmt.Sprintf("android:%x-%x-%x-%x-%x",
		hash[0:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}
