package vault

import "testing"

func TestRoundTrip(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := v.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "secret" {
		t.Fatal("plaintext was not encrypted")
	}
	plain, err := v.Decrypt(encrypted)
	if err != nil || plain != "secret" {
		t.Fatalf("round trip: %q %v", plain, err)
	}
}
