package renders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

// Upload POSTs one capture to {backendURL}/v1/renders as multipart/form-data
// with exactly two parts, in order: "capture" (JSON metadata) then "image"
// (PNG bytes). The order is the wire contract, not a style choice — the
// backend authorizes the request from the capture part before it reads a
// byte of the image part, so an unauthorized caller's image bytes are never
// decoded on its behalf (lgty-backend internal/renders/handler.go). This
// client preserves that order for the same reason: sending anything else
// first would misrepresent what actually happens on the wire.
func Upload(ctx context.Context, backendURL, oidcToken string, meta CaptureMetadata, img Image) (CaptureAck, error) {
	contentType, body, err := buildMultipart(meta, img)
	if err != nil {
		return CaptureAck{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/v1/renders", body)
	if err != nil {
		return CaptureAck{}, err
	}
	req.Header.Set("Content-Type", contentType)
	if oidcToken != "" {
		req.Header.Set("Authorization", "Bearer "+oidcToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CaptureAck{}, fmt.Errorf("post capture for state %q index %d: %w", meta.StateID, meta.CaptureIndex, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return CaptureAck{}, fmt.Errorf("upload failed: %s: %s", resp.Status, describeError(respBody))
	}
	var ack CaptureAck
	if err := json.Unmarshal(respBody, &ack); err != nil {
		return CaptureAck{}, fmt.Errorf("decode upload response: %w: %s", err, respBody)
	}
	return ack, nil
}

// Complete POSTs {backendURL}/v1/renders/complete, telling the backend this
// CI run has uploaded every capture it is going to for commitSHA. This is
// what triggers the second pass that upgrades an already-published brief
// in place — nothing polls, and if this call is never made the brief's
// rendered-state check simply stays declared not-run, which is the honest
// state absent this signal.
func Complete(ctx context.Context, backendURL, oidcToken, commitSHA string) (CompletionAck, error) {
	body, err := json.Marshal(CompletionRequest{CommitSHA: commitSHA})
	if err != nil {
		return CompletionAck{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/v1/renders/complete", bytes.NewReader(body))
	if err != nil {
		return CompletionAck{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if oidcToken != "" {
		req.Header.Set("Authorization", "Bearer "+oidcToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CompletionAck{}, fmt.Errorf("post completion: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return CompletionAck{}, fmt.Errorf("completion failed: %s: %s", resp.Status, describeError(respBody))
	}
	var ack CompletionAck
	if err := json.Unmarshal(respBody, &ack); err != nil {
		return CompletionAck{}, fmt.Errorf("decode completion response: %w: %s", err, respBody)
	}
	return ack, nil
}

func buildMultipart(meta CaptureMetadata, img Image) (string, *bytes.Buffer, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return "", nil, fmt.Errorf("encode capture metadata: %w", err)
	}
	capturePart, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="capture"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		return "", nil, err
	}
	if _, err := capturePart.Write(metaJSON); err != nil {
		return "", nil, err
	}

	imagePart, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="image"; filename="capture.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		return "", nil, err
	}
	if _, err := imagePart.Write(img.Bytes); err != nil {
		return "", nil, err
	}

	if err := w.Close(); err != nil {
		return "", nil, err
	}
	return w.FormDataContentType(), body, nil
}

// describeError renders the backend's {code, message} error body when
// present, falling back to the raw bytes so nothing is ever silently
// swallowed.
func describeError(body []byte) string {
	var we wireError
	if json.Unmarshal(body, &we) == nil && we.Message != "" {
		if we.Code != "" {
			return fmt.Sprintf("%s: %s", we.Code, we.Message)
		}
		return we.Message
	}
	return string(body)
}
