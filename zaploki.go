package zaploki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

type ZapLoki interface {
	Hook() func(zapcore.Entry, error)
	Stop()
}

type Config struct {
	Url          string
	BatchMaxSize int
	BatchMaxWait time.Duration
}

type lokiHook struct {
	config    *Config
	ctx       context.Context
	client    *http.Client
	quit      chan struct{}
	entries   chan *zapcore.Entry
	waitGroup sync.WaitGroup
}

type lokiPushRequest struct {
	Streams []*stream `json:"streams"`
}

type stream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func New(ctx context.Context, cfg Config) ZapLoki {
	c := &http.Client{}

	hook := &lokiHook{
		config:  &cfg,
		ctx:     ctx,
		client:  c,
		quit:    make(chan struct{}),
		entries: make(chan *zapcore.Entry),
	}

	hook.waitGroup.Add(1)
	go hook.run()
	return hook
}

func (z *lokiHook) Hook() func(zapcore.Entry, error) {

	return nil
}

func (z *lokiHook) Stop() {
	close(z.quit)
	z.waitGroup.Wait()
}

func (z *lokiHook) run() {
	var batch []*zapcore.Entry
	ticker := time.NewTimer(z.config.BatchMaxWait)

	defer func() {
		if len(batch) > 0 {
			z.send(batch)
		}

		z.waitGroup.Done()
	}()

	for {
		select {
		case <-z.ctx.Done():
			return
		case <-z.quit:
			return
		case entry := <-z.entries:
			batch = append(batch, entry)
			if len(batch) >= z.config.BatchMaxSize {
				z.send(batch)
				batch = make([]*zapcore.Entry, 0)
				ticker.Reset(z.config.BatchMaxWait)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				z.send(batch)
				batch = make([]*zapcore.Entry, 0)
			}
			ticker.Reset(z.config.BatchMaxWait)
		}
	}
}

func (z *lokiHook) send(batch []*zapcore.Entry) error {
	var streams []*stream
	streams = append(streams, &stream{
		Stream: map[string]string{},
		Values: make([][2]string, 0),
	})
	data := lokiPushRequest{
		Streams: streams,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	req, err := http.NewRequest("POST", z.config.Url, bytes.NewBuffer(msg))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := z.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	return nil
}
