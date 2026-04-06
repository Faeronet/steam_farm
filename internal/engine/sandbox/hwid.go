package sandbox

import (
	"crypto/rand"
	"fmt"
)

func GenerateMachineID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func GenerateMAC() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	b[0] = (b[0] | 0x02) & 0xFE // locally administered, unicast
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func GenerateHostname(accountID int64) string {
	return fmt.Sprintf("farm-bot-%d", accountID)
}
