package auth

import "testing"

func TestGenerateSessionToken(t *testing.T) {
	token, err := GenerateSessionToken()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(token) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(token))
	}

	anotherToken, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	if token == anotherToken {
		t.Fatal("generated tokens should be unique")
	}
}

func TestHashSessionToken(t *testing.T) {
	token := "example-session-token"

	hash := HashSessionToken(token)

	if hash == token {
		t.Fatal("token should be hashed")
	}

	if len(hash) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(hash))
	}

	if hash != HashSessionToken(token) {
		t.Fatal("same token should produce the same hash")
	}
}
