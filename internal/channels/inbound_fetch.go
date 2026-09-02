package channels

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const inboundMediaMaxBytes = 25 * 1024 * 1024

var inboundHTTPClient = &http.Client{Timeout: 45 * time.Second}

func fetchInboundBytes(rawURL string, header http.Header) ([]byte, string, error) {
	if rawURL == "" {
		return nil, "", fmt.Errorf("empty download url")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := inboundHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, inboundMediaMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(data) > inboundMediaMaxBytes {
		return nil, "", fmt.Errorf("resource exceeds 25MB")
	}
	return data, resp.Header.Get("Content-Type"), nil
}
