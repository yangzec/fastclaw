package users

import "testing"

func TestCheckPasswordLength(t *testing.T) {
	if err := CheckPasswordLength(""); err == nil {
		t.Fatal("empty password accepted")
	}
	if err := CheckPasswordLength("1234567"); err == nil {
		t.Fatal("7-char password accepted")
	}
	if err := CheckPasswordLength("12345678"); err != nil {
		t.Fatalf("8-char password rejected: %v", err)
	}
}
