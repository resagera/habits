package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidInitData = errors.New("invalid init data")
	ErrExpiredInitData = errors.New("init data expired")
)

type TgUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// ValidateInitData verifies the HMAC signature of a Telegram Mini App
// initData string and returns the embedded user.
// See https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func ValidateInitData(initData, botToken string, maxAge time.Duration, now time.Time) (TgUser, error) {
	var user TgUser

	values, err := url.ParseQuery(initData)
	if err != nil {
		return user, fmt.Errorf("%w: %v", ErrInvalidInitData, err)
	}
	gotHash := values.Get("hash")
	if gotHash == "" {
		return user, fmt.Errorf("%w: missing hash", ErrInvalidInitData)
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(pairs, "\n")

	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	wantHash := hex.EncodeToString(hmacSHA256(secret, []byte(dataCheckString)))
	if !hmac.Equal([]byte(wantHash), []byte(gotHash)) {
		return user, fmt.Errorf("%w: hash mismatch", ErrInvalidInitData)
	}

	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return user, fmt.Errorf("%w: bad auth_date", ErrInvalidInitData)
	}
	if now.Sub(time.Unix(authDate, 0)) > maxAge {
		return user, ErrExpiredInitData
	}

	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return user, fmt.Errorf("%w: bad user field", ErrInvalidInitData)
	}
	return user, nil
}

func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}
