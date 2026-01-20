package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"manager-server/internal/config"
)

type httpWorkflowRuntimeClient struct {
	baseURL  string
	client   *http.Client
	apiToken string
}

// NewWorkflowRuntimeClient builds an HTTP client for the backend workflow runtime.
func NewWorkflowRuntimeClient(cfg config.WorkflowRuntimeConfig) WorkflowRuntimeClient {
	if cfg.BaseURL == "" {
		return nil
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	timeout := time.Duration(cfg.TimeoutSeconds)
	if timeout <= 0 {
		timeout = 15
	}
	return &httpWorkflowRuntimeClient{
		baseURL:  base,
		apiToken: cfg.APIToken,
		client: &http.Client{
			Timeout: timeout * time.Second,
		},
	}
}

func (c *httpWorkflowRuntimeClient) Execute(ctx context.Context, req *WorkflowTestRequest) (*WorkflowTestResult, error) {
	if c == nil {
		return nil, fmt.Errorf("workflow runtime client not configured")
	}
	body := map[string]any{
		"workflowId": req.WorkflowID,
		"definition": req.Definition,
		"input":      req.Input,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/workflows/execute", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiToken != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiToken))
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("workflow runtime returned %d", resp.StatusCode)
	}
	var envelope struct {
		Code json.Number         `json:"code"`
		Data *WorkflowTestResult `json:"data"`
		Msg  string              `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Code.String() != "0" && envelope.Code.String() != "" {
		return nil, fmt.Errorf("workflow runtime error: %s", envelope.Msg)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("workflow runtime returned empty body")
	}
	return envelope.Data, nil
}
