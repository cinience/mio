package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"backend-server/internal/config"
	"backend-server/internal/app/service"
)

func TestHandleImageGeneration_DefaultConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"ZGF0YQ=="}]}`))
	}))
	defer openaiServer.Close()

	config.GlobalConfig = &config.AppConfig{
		Image: config.ImageGenerationConfig{
			Provider: "openai_image",
			Providers: map[string]config.ImageProviderConfig{
				"openai_image": {
					Type:           "openai",
					Model:          "gpt-image-1",
					APIKey:         "test-key",
					BaseURL:        openaiServer.URL + "/v1",
					ResponseFormat: "b64_json",
				},
			},
		},
	}
	defer func() { config.GlobalConfig = nil }()

	service.SetDefaultRegistry(service.NewRegistry())

	handler := NewHandler()
	router := gin.New()
	handler.RegisterRoutes(router)

	reqBody := `{"prompt":"sunset"}`
	req, _ := http.NewRequest(http.MethodPost, "/xiaozhi/api/images/generations", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", "device-1")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d - %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Created int `json:"created"`
		Data    []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(payload.Data) != 1 || payload.Data[0].B64JSON == "" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestHandleImageGeneration_EmptyPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config.GlobalConfig = &config.AppConfig{}
	defer func() { config.GlobalConfig = nil }()
	service.SetDefaultRegistry(service.NewRegistry())

	handler := NewHandler()
	router := gin.New()
	handler.RegisterRoutes(router)

	reqBody := `{"prompt":"   "}`
	req, _ := http.NewRequest(http.MethodPost, "/xiaozhi/api/images/generations", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", "device-1")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}
