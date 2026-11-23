package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// HTTP is a Transcriber that speaks the OpenAI audio API
// (POST {URL}/audio/transcriptions), which OpenAI, and the common self-hosted
// Whisper servers (whisper.cpp's server, faster-whisper-server, LocalAI),
// all answer. URL is the API's base, such as https://api.openai.com/v1 or
// http://whisper.internal:8080/v1.
type HTTP struct {
	URL    string
	Model  string // "whisper-1" if empty
	APIKey string // sent as a bearer token when set; a local server wants none
	// Language, if set, is an ISO-639-1 hint ("en"); empty lets the service detect it.
	Language string
	Client   *http.Client // a 5-minute timeout client if nil
}

// maxError is how much of a failing answer is kept in the error.
const maxError = 300

func (h *HTTP) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Transcribe sends the piece and reads the answer's segments. The API key is
// never put in an error or a log line.
func (h *HTTP) Transcribe(ctx context.Context, wav []byte, name string) (Result, error) {
	if strings.TrimSpace(h.URL) == "" {
		return Result{}, errors.New("transcribe: no URL")
	}
	model := h.Model
	if model == "" {
		model = "whisper-1"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(name)))
	hdr.Set("Content-Type", "audio/wav")
	fw, err := mw.CreatePart(hdr)
	if err != nil {
		return Result{}, err
	}
	if _, err := fw.Write(wav); err != nil {
		return Result{}, err
	}
	fields := map[string]string{"model": model, "response_format": "verbose_json"}
	if h.Language != "" {
		fields["language"] = h.Language
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return Result{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return Result{}, err
	}

	url := strings.TrimRight(h.URL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}
	resp, err := h.client().Do(req)
	if err != nil {
		// net/http errors name the URL, never the headers: safe to return.
		return Result{}, fmt.Errorf("transcribe: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Result{}, fmt.Errorf("transcribe: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > maxError {
			msg = msg[:maxError] + "…"
		}
		return Result{}, fmt.Errorf("transcribe: %s: %s", resp.Status, msg)
	}
	var out struct {
		Language string `json:"language"`
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("transcribe: the service's answer isn't JSON: %w", err)
	}
	res := Result{Language: out.Language, Text: strings.TrimSpace(out.Text), Segments: []Segment{}}
	for _, s := range out.Segments {
		if t := strings.TrimSpace(s.Text); t != "" {
			res.Segments = append(res.Segments, Segment{Start: s.Start, End: s.End, Text: t})
		}
	}
	// A service that gave text and no segments (response_format ignored) still
	// gave the words: one segment over the whole piece's start.
	if len(res.Segments) == 0 && res.Text != "" {
		res.Segments = append(res.Segments, Segment{Text: res.Text})
	}
	return res, nil
}

func escapeQuotes(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }
