package store

import "testing"

func TestSecretRoundTrip(t *testing.T) {
	SetAutomationKey("test-key-жар")
	for _, plain := range []string{"", "user@example.com", "п@ссворд-123", "x"} {
		enc, err := encryptSecret(plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		if plain != "" && enc == plain {
			t.Fatalf("ciphertext равен тексту для %q", plain)
		}
		dec, err := decryptSecret(enc)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plain, err)
		}
		if dec != plain {
			t.Fatalf("round-trip: got %q, want %q", dec, plain)
		}
	}
}

func TestSecretWrongKey(t *testing.T) {
	SetAutomationKey("key-one")
	enc, _ := encryptSecret("secret")
	SetAutomationKey("key-two")
	if _, err := decryptSecret(enc); err == nil {
		t.Fatal("расшифровка чужим ключом должна падать")
	}
	SetAutomationKey("key-one")
}

func TestSetCredentials(t *testing.T) {
	SetAutomationKey("k")
	var c AutomationConfig
	if err := c.SetCredentials("login1", "pass1"); err != nil {
		t.Fatal(err)
	}
	if c.LoginEnc == "" || c.PasswordEnc == "" {
		t.Fatal("креды не зашифрованы")
	}
	// пустые значения не затирают существующие
	prevL, prevP := c.LoginEnc, c.PasswordEnc
	if err := c.SetCredentials("", ""); err != nil {
		t.Fatal(err)
	}
	if c.LoginEnc != prevL || c.PasswordEnc != prevP {
		t.Fatal("пустой апдейт затёр креды")
	}
	l, p, err := c.Creds()
	if err != nil {
		t.Fatal(err)
	}
	if l != "login1" || p != "pass1" {
		t.Fatalf("creds: %q %q", l, p)
	}
}
