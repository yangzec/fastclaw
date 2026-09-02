package channels

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestLINEDownloadMessageContent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/bot/message/mid-1/content" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("line-pdf"))
	}))
	t.Cleanup(srv.Close)

	l := &LINE{channelToken: "tok-1", dataAPIBase: srv.URL}
	item, err := l.downloadMessageContent(&LINEMessage{Type: "file", ID: "mid-1", FileName: "notes.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if item.Filename != "notes.pdf" || string(item.Bytes) != "line-pdf" || item.ContentType != "application/pdf" {
		t.Fatalf("item = %#v", item)
	}
}

func TestLINEDispatchEventForwardsFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	t.Cleanup(srv.Close)

	mb := bus.New()
	l := &LINE{
		bus:          mb,
		accountID:    "Ubot",
		channelToken: "tok",
		dataAPIBase:  srv.URL,
		replyTokens:  map[string]lineReplyToken{},
	}
	l.dispatchEvent(LINEEvent{
		Type:    "message",
		Source:  LINESource{Type: "user", UserID: "Ualice"},
		Message: &LINEMessage{Type: "image", ID: "img-1"},
	})
	got := <-mb.Inbound
	if got.Channel != "line" || got.ChatID != "Ualice" || len(got.MediaItems) != 1 {
		t.Fatalf("inbound = %+v", got)
	}
	if string(got.MediaItems[0].Bytes) != "jpeg-bytes" {
		t.Fatalf("media = %#v", got.MediaItems)
	}
	if got.Text != "请查看我发送的附件。" {
		t.Fatalf("text = %q", got.Text)
	}
}
