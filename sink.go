package zaploki

import (
	"encoding/json"
)

type lokiSink interface {
	Sync() error
	Close() error
	Write(p []byte) (int, error)
}

// type lokiSink struct{}
type sink struct {
	lokiPusher *lokiPusher
}

func newSink(lp *lokiPusher) lokiSink {
	return sink{
		lokiPusher: lp,
	}
}

func (s sink) Sync() error {
	if len(s.lokiPusher.logsBatch) > 0 {
		return s.lokiPusher.send()
	}
	return nil
}
func (s sink) Close() error {
	s.lokiPusher.Stop()
	return nil
}

func (s sink) Write(p []byte) (int, error) {
	var entry logEntry
	err := json.Unmarshal(p, &entry)
	if err != nil {
		return 0, err
	}
	entry.raw = string(p)
	s.lokiPusher.entry <- entry
	return len(p), nil
}
