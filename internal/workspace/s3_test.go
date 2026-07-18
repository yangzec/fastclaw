package workspace

import (
	"context"
	"testing"
)

func TestS3PublicURLUsesPublicBaseAndEscapesOnce(t *testing.T) {
	st := &S3{prefix: "fast claw", publicBaseURL: "https://cdn.example.test/base path/"}
	got, err := st.PublicURL(context.Background(), "agent a", "", "session 1", "generated-images/a b.png")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://cdn.example.test/base%20path/fast%20claw/agent%20a/sessions/session%201/generated-images/a%20b.png"
	if got != want {
		t.Fatalf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PublicURLUnsupportedWhenBaseMissing(t *testing.T) {
	st := &S3{}
	if _, err := st.PublicURL(context.Background(), "agent", "", "session", "file.png"); err != ErrSignedURLUnsupported {
		t.Fatalf("err = %v, want ErrSignedURLUnsupported", err)
	}
}
