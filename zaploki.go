package zaploki

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ZapLoki interface {
	Hook(e zapcore.Entry) error
	Sink(u *url.URL) (zap.Sink, error)
	Stop()
	WithCreateLogger(zap.Config) (*zap.Logger, error)
}

type Config struct {
	// SinkKey is the key that is used to register the sink with zap
	SinkKey string
	// Url of the loki server including http:// or https://
	Url string
	// BatchMaxSize is the maximum number of log lines that are sent in one request
	BatchMaxSize int
	// BatchMaxWait is the maximum time to wait before sending a request
	BatchMaxWait time.Duration
	// Labels that are added to all log lines
	Labels   map[string]string
	Username string
	Password string
}

type lokiPusher struct {
	config    *Config
	ctx       context.Context
	cancel    context.CancelFunc
	client    *http.Client
	quit      chan struct{}
	entry     chan logEntry
	waitGroup sync.WaitGroup
	logsBatch []streamValue
}

type lokiPushRequest struct {
	Streams []stream `json:"streams"`
}

type stream struct {
	Stream map[string]string `json:"stream"`
	Values []streamValue     `json:"values"`
}

type streamValue []string

type logEntry struct {
	Level     string  `json:"level"`
	Timestamp float64 `json:"ts"`
	Message   string  `json:"msg"`
	Caller    string  `json:"caller"`
	raw       string
}

func New(ctx context.Context, cfg Config) ZapLoki {
	cfg.Url = fmt.Sprintf("%s/loki/api/v1/push", strings.TrimSuffix(cfg.Url, "/"))

	ctx, cancel := context.WithCancel(ctx)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	lp := &lokiPusher{
		config:    &cfg,
		ctx:       ctx,
		cancel:    cancel,
		client:    &http.Client{},
		quit:      make(chan struct{}),
		entry:     make(chan logEntry),
		logsBatch: make([]streamValue, 0, cfg.BatchMaxSize),
	}

	lp.waitGroup.Add(1)
	go lp.run()
	return lp
}

// Hook is a function that can be used as a zap hook to write log lines to loki
func (lp *lokiPusher) Hook(e zapcore.Entry) error {
	lp.entry <- logEntry{
		Level:     e.Level.String(),
		Timestamp: float64(e.Time.UnixMilli()),
		Message:   e.Message,
		Caller:    e.Caller.TrimmedPath(),
	}
	return nil
}

// Sink returns a new loki zap sink
func (lp *lokiPusher) Sink(_ *url.URL) (zap.Sink, error) {
	return newSink(lp), nil
}

// Stop stops the loki pusher
func (lp *lokiPusher) Stop() {
	close(lp.quit)
	lp.waitGroup.Wait()
	lp.cancel()
}

// WithCreateLogger creates a new zap logger with a loki sink from a zap config
func (lp *lokiPusher) WithCreateLogger(cfg zap.Config) (*zap.Logger, error) {
	if lp.config.SinkKey == "" {
		lp.config.SinkKey = "loki"
	}
	err := zap.RegisterSink(lp.config.SinkKey, lp.Sink)
	if err != nil {
		log.Fatal(err)
	}

	fullSinkKey := fmt.Sprintf("%s://", lp.config.SinkKey)

	if cfg.OutputPaths == nil {
		cfg.OutputPaths = []string{fullSinkKey}
	} else {
		cfg.OutputPaths = append(cfg.OutputPaths, fullSinkKey)
	}

	return cfg.Build()
}

func (lp *lokiPusher) run() {
	ticker := time.NewTicker(lp.config.BatchMaxWait)
	defer ticker.Stop()

	defer func() {
		if len(lp.logsBatch) > 0 {
			err := lp.send()
			if err != nil {
				slog.Error("failed to send logs", slog.Any("error", err))
			}
		}

		lp.waitGroup.Done()
	}()

	for {
		select {
		case <-lp.ctx.Done():
			return
		case <-lp.quit:
			return
		case entry := <-lp.entry:
			lp.logsBatch = append(lp.logsBatch, newLog(entry))
			if len(lp.logsBatch) >= lp.config.BatchMaxSize {
				err := lp.send()
				if err != nil {
					slog.Error("failed to send logs", slog.Any("error", err))
				}
				lp.logsBatch = lp.logsBatch[:0]
			}
		case <-ticker.C:
			if len(lp.logsBatch) > 0 {
				err := lp.send()
				if err != nil {
					slog.Error("failed to send logs", slog.Any("error", err))
				}
				lp.logsBatch = lp.logsBatch[:0]
			}
		}
	}
}

func newLog(entry logEntry) streamValue {
	ts := time.Unix(int64(entry.Timestamp), 0)
	return []string{strconv.FormatInt(ts.UnixNano(), 10), entry.raw}
}

func (lp *lokiPusher) send() error {
	buf := bytes.NewBuffer([]byte{})
	gz := gzip.NewWriter(buf)

	if err := json.NewEncoder(gz).Encode(lokiPushRequest{Streams: []stream{{
		Stream: lp.config.Labels,
		Values: lp.logsBatch,
	}}}); err != nil {
		return err
	}

	if err := gz.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, lp.config.Url, buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.WithContext(lp.ctx)

	if lp.config.Username != "" && lp.config.Password != "" {
		req.SetBasicAuth(lp.config.Username, lp.config.Password)
	}

	resp, err := lp.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("recieved unexpected response code from Loki: %s", resp.Status)
	}

	return nil
}
