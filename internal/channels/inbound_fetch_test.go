package channels

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/slack-go/slack"
)

func TestFetchInboundBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("pdf-bytes"))
	}))
	t.Cleanup(srv.Close)
	data, ctype, err := fetchInboundBytes(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pdf-bytes" || ctype != "application/pdf" {
		t.Fatalf("data=%q ctype=%q", data, ctype)
	}
}

func TestDiscordInboundAttachments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)
	items := discordInboundAttachments([]*discordgo.MessageAttachment{{
		Filename:    "shot.png",
		URL:         srv.URL,
		ContentType: "image/png",
	}})
	if len(items) != 1 || items[0].Filename != "shot.png" || string(items[0].Bytes) != "png-bytes" {
		t.Fatalf("items = %#v", items)
	}
}

func TestSlackDownloadFilesUsesBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("slack-file"))
	}))
	t.Cleanup(srv.Close)
	s := &Slack{botToken: "xoxb-test"}
	items := s.downloadSlackFiles([]slack.File{{
		Name:               "notes.pdf",
		Mimetype:           "application/pdf",
		URLPrivateDownload: srv.URL,
	}})
	if gotAuth != "Bearer xoxb-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if len(items) != 1 || string(items[0].Bytes) != "slack-file" {
		t.Fatalf("items = %#v", items)
	}
}
