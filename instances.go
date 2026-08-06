package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Instance is the VISA instance returned by the API (response.data).
type Instance struct {
	IP string `json:"ipAddress"`
}

type instanceEnvelope struct {
	Data Instance `json:"data"`
}

// APIClient talks to the VISA API server.
type APIClient struct {
	base string
	http *http.Client
}

func NewAPIClient(base string) *APIClient {
	return &APIClient{base: base, http: &http.Client{Timeout: 30 * time.Second}}
}

// GetInstance authorizes (user, instance) and returns the instance. ctx is the
// inbound request's context, so a client disconnect cancels the upstream call.
// The second return is a proxy status: 0 on success, otherwise the HTTP status to
// return to the client (the API's own status is propagated; a transport error or
// an unusable response -> 500).
func (c *APIClient) GetInstance(ctx context.Context, id, token string) (*Instance, int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/jupyter/instances/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}
	var env instanceEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, http.StatusInternalServerError
	}
	// Without this an instance with no IP yet (building/stopped) would make the
	// target host ":{remotePort}", which dials the proxy's own localhost.
	if env.Data.IP == "" {
		log.Printf("instance %s has no ipAddress", id)
		return nil, http.StatusBadGateway
	}
	return &env.Data, 0
}

// NotebookOpen notifies the API a notebook session started (with bearer auth).
func (c *APIClient) NotebookOpen(id, kernelID, sessionID, token string) error {
	return c.postNotebook("open", id, kernelID, sessionID, token)
}

// NotebookClose notifies the API a notebook session ended. token may be empty
// (startup zombie cleanup has no token), in which case no Authorization is sent.
func (c *APIClient) NotebookClose(id, kernelID, sessionID, token string) error {
	return c.postNotebook("close", id, kernelID, sessionID, token)
}

func (c *APIClient) postNotebook(action, id, kernelID, sessionID, token string) error {
	body, _ := json.Marshal(map[string]string{"kernelId": kernelID, "sessionId": sessionID})
	// Deliberately detached from the inbound request's context, unlike
	// GetInstance: a notebook/close has to outlive the WebSocket whose teardown
	// triggered it. c.http's timeout is what bounds it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		fmt.Sprintf("%s/jupyter/instances/%s/notebook/%s", c.base, id, action),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notebook/%s returned %d", action, resp.StatusCode)
	}
	return nil
}
