package positions

import (
	"fmt"
	"time"
)

type realTicker struct {
	ticker *time.Ticker
}

func NewTicker(interval time.Duration) (Ticker, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("check interval must be positive, got %s", interval)
	}

	return &realTicker{ticker: time.NewTicker(interval)}, nil
}

func (t *realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *realTicker) Stop() {
	t.ticker.Stop()
}
