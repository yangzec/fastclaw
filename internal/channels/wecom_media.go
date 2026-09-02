package channels

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func (w *WeCom) sendMediaItem(chatID, reqID string, chatType int, item bus.MediaItem) error {
	if len(item.Bytes) < 5 {
		return fmt.Errorf("wecom media %q is empty", item.Filename)
	}
	kind := wecomMediaKind(item)
	if err := wecomMediaSizeOK(kind, len(item.Bytes)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wecomMediaSendTimeout)
	defer cancel()
	mediaID, err := w.uploadMedia(ctx, kind, item)
	if err != nil {
		return err
	}
	body := map[string]any{
		"msgtype": kind,
		kind:      map[string]any{"media_id": mediaID},
	}
	if reqID != "" {
		return w.request(ctx, wecomCmdRespond, reqID, body)
	}
	if chatID == "" {
		return fmt.Errorf("wecom: chat id required for proactive media send")
	}
	body["chatid"] = chatID
	if chatType == 1 || chatType == 2 {
		body["chat_type"] = chatType
	}
	return w.request(ctx, wecomCmdSend, wecomNewReqID(), body)
}

const wecomMediaSendTimeout = 90 * time.Second

func (w *WeCom) uploadMedia(ctx context.Context, kind string, item bus.MediaItem) (string, error) {
	chunk := wecomMediaChunkBytes
	total := (len(item.Bytes) + chunk - 1) / chunk
	if total < 1 {
		total = 1
	}
	if total > wecomMaxMediaChunks {
		return "", fmt.Errorf("wecom media %q needs %d chunks (max %d)", item.Filename, total, wecomMaxMediaChunks)
	}
	sum := md5.Sum(item.Bytes)
	initFr, err := w.requestFrame(ctx, wecomCmdUploadInit, wecomNewReqID(), map[string]any{
		"type":         kind,
		"filename":     item.Filename,
		"total_size":   len(item.Bytes),
		"total_chunks": total,
		"md5":          hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return "", err
	}
	var init struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(initFr.Body, &init); err != nil || init.UploadID == "" {
		return "", fmt.Errorf("wecom upload init: missing upload_id")
	}
	for i := 0; i < total; i++ {
		end := (i + 1) * chunk
		if end > len(item.Bytes) {
			end = len(item.Bytes)
		}
		if err := w.request(ctx, wecomCmdUploadChunk, wecomNewReqID(), map[string]any{
			"upload_id":   init.UploadID,
			"chunk_index": strconv.Itoa(i),
			"base64_data": base64.StdEncoding.EncodeToString(item.Bytes[i*chunk : end]),
		}); err != nil {
			return "", err
		}
	}
	finFr, err := w.requestFrame(ctx, wecomCmdUploadFinish, wecomNewReqID(), map[string]any{
		"upload_id": init.UploadID,
	})
	if err != nil {
		return "", err
	}
	var fin struct {
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(finFr.Body, &fin); err != nil || fin.MediaID == "" {
		return "", fmt.Errorf("wecom upload finish: missing media_id")
	}
	return fin.MediaID, nil
}

func wecomMediaKind(item bus.MediaItem) string {
	ext := strings.ToLower(filepath.Ext(item.Filename))
	ct := strings.ToLower(item.ContentType)
	switch {
	case ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif",
		ct == "image/png" || ct == "image/jpeg" || ct == "image/gif":
		return "image"
	case ext == ".mp4" || ct == "video/mp4":
		return "video"
	case ext == ".amr" || ct == "audio/amr":
		return "voice"
	default:
		return "file"
	}
}

func wecomMediaSizeOK(kind string, n int) error {
	max := 20 << 20
	switch kind {
	case "image", "video":
		max = 10 << 20
	case "voice":
		max = 2 << 20
	}
	if n > max {
		return fmt.Errorf("wecom %s exceeds %d bytes", kind, max)
	}
	return nil
}
