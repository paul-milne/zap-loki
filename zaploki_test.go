package zaploki

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testServer(t *testing.T, test func(t *testing.T, req lokiPushRequest)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method, "Expected POST request")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"), "Expected Content-Type application/json")
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"), "Expected Content-Encoding gzip")

		var req lokiPushRequest
		gz, err := gzip.NewReader(r.Body)
		assert.NoError(t, err, "Failed to create gzip reader")

		defer gz.Close()
		assert.NoError(t, json.NewDecoder(gz).Decode(&req), "Failed to decode json from gzip")

		test(t, req)

		w.WriteHeader(http.StatusNoContent)
	}))
}

func TestNew(t *testing.T) {
	mockServer := testServer(t, func(t *testing.T, req lokiPushRequest) {
		assert.Len(t, req.Streams, 1, "Expected one stream")
		if len(req.Streams) == 0 {
			t.Errorf("Expected at least one stream, got %d", len(req.Streams))
		}
		expectedLabels := map[string]string{"app": "test", "env": "dev"}
		assert.Equal(t, expectedLabels, req.Streams[0].Stream, "Expected labels to match")
	})
	defer mockServer.Close()
	v := New(context.Background(), Config{
		Url:          mockServer.URL,
		BatchMaxSize: 100,
		BatchMaxWait: 10 * time.Second,
		Labels:       map[string]string{"app": "test", "env": "dev"},
	})
	logger, err := v.WithCreateLogger(zap.NewProductionConfig())
	if err != nil {
		t.Fatal(err)
	}

	logger.Info("test message", zap.String("key", "value"))
	defer logger.Sync()
}
