package guard

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"time"
)

const steamAlphabet = "23456789BCDFGHJKMNPQRTVWXY"

func GenerateSteamTOTP(sharedSecret string) (string, error) {
	return GenerateSteamTOTPAt(sharedSecret, time.Now())
}

func GenerateSteamTOTPAt(sharedSecret string, t time.Time) (string, error) {
	secret, err := base64.StdEncoding.DecodeString(sharedSecret)
	if err != nil {
		return "", err
	}

	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(t.Unix()/30))

	mac := hmac.New(sha1.New, secret)
	mac.Write(timeBytes)
	hash := mac.Sum(nil)

	offset := hash[len(hash)-1] & 0x0F
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7FFFFFFF

	code := make([]byte, 5)
	for i := 0; i < 5; i++ {
		code[i] = steamAlphabet[truncated%uint32(len(steamAlphabet))]
		truncated /= uint32(len(steamAlphabet))
	}

	return string(code), nil
}

func SecondsUntilRefresh() int {
	return 30 - int(time.Now().Unix()%30)
}
