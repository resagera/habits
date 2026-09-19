package auth

import (
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBotToken = "1234567890:TEST-TOKEN-abcdefghij"

// signInitData builds a valid initData string the same way Telegram does.
func signInitData(params url.Values, botToken string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+params.Get(k))
	}
	dcs := strings.Join(pairs, "\n")
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	params.Set("hash", hex.EncodeToString(hmacSHA256(secret, []byte(dcs))))
	return params.Encode()
}

func validParams(authDate time.Time) url.Values {
	return url.Values{
		"auth_date": {strconv.FormatInt(authDate.Unix(), 10)},
		"query_id":  {"AAF03QwAAAAAAHTdDAbpQqNZ"},
		"user":      {`{"id":42,"first_name":"Alice","username":"alice_w","language_code":"ru"}`},
	}
}

func TestValidateInitData_OK(t *testing.T) {
	now := time.Now()
	initData := signInitData(validParams(now), testBotToken)

	user, err := ValidateInitData(initData, testBotToken, 24*time.Hour, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 42 || user.FirstName != "Alice" || user.Username != "alice_w" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestValidateInitData_WrongToken(t *testing.T) {
	now := time.Now()
	initData := signInitData(validParams(now), "other:token")

	_, err := ValidateInitData(initData, testBotToken, 24*time.Hour, now)
	if !errors.Is(err, ErrInvalidInitData) {
		t.Fatalf("want ErrInvalidInitData, got %v", err)
	}
}

func TestValidateInitData_TamperedUser(t *testing.T) {
	now := time.Now()
	initData := signInitData(validParams(now), testBotToken)
	tampered := strings.Replace(initData, "%22id%22%3A42", "%22id%22%3A43", 1)
	if tampered == initData {
		t.Fatal("test setup: substring not found")
	}

	_, err := ValidateInitData(tampered, testBotToken, 24*time.Hour, now)
	if !errors.Is(err, ErrInvalidInitData) {
		t.Fatalf("want ErrInvalidInitData, got %v", err)
	}
}

func TestValidateInitData_Expired(t *testing.T) {
	now := time.Now()
	initData := signInitData(validParams(now.Add(-25*time.Hour)), testBotToken)

	_, err := ValidateInitData(initData, testBotToken, 24*time.Hour, now)
	if !errors.Is(err, ErrExpiredInitData) {
		t.Fatalf("want ErrExpiredInitData, got %v", err)
	}
}

func TestValidateInitData_MissingHash(t *testing.T) {
	_, err := ValidateInitData("auth_date=1&user=%7B%22id%22%3A1%7D", testBotToken, 24*time.Hour, time.Now())
	if !errors.Is(err, ErrInvalidInitData) {
		t.Fatalf("want ErrInvalidInitData, got %v", err)
	}
}
