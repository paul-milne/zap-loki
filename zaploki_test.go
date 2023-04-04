package zaploki

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNew(t *testing.T) {
	v := New(context.Background(), Config{
		Url:          "http://localhost:3100",
		BatchMaxSize: 100,
		BatchMaxWait: 10 * time.Second,
		Labels:       []string{"app=app1", "env=dev"},
	})
	logger, err := v.WithCreateLogger(zap.NewProductionConfig())
	if err != nil {
		t.Fatal(err)
	}
	// logger = logger.WithOptions(zap.Hooks(v.Hook))

	logger.Info("failed to fetch URL",
		// Structured context as strongly typed Field values.
		zap.String("url", "https.//test.com"),
		zap.Int("attempt", 3),
		zap.Duration("backoff", time.Second),
	)
	defer logger.Sync()

	time.Sleep(30 * time.Minute)
}
