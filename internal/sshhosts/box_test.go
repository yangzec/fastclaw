package sshhosts

import (
	"testing"
)

func TestBoxRoundTrip(t *testing.T) {
	box, err := OpenBoxAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Seal(Creds{Password: "s3cret", PrivateKey: "KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || enc == "s3cret" {
		t.Fatalf("ciphertext looks raw: %q", enc)
	}
	got, err := box.Open(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "s3cret" || got.PrivateKey != "KEY" {
		t.Fatalf("got %+v", got)
	}
}

func TestBoxRejectsTamper(t *testing.T) {
	box, err := OpenBoxAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Seal(Creds{Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	tampered := enc[:len(enc)-2] + "ff"
	if _, err := box.Open(tampered); err == nil {
		t.Fatal("expected decrypt failure")
	}
}
