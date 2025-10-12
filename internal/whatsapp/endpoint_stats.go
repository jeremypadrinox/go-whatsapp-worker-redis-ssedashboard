package whatsapp

import (
	"sync"
	"sync/atomic"
	"time"
)

type EndpointStats struct {
	URL               string
	MessagesSent      int64
	MessagesFailed    int64
	TotalResponseTime int64 // milliseconds
	LastUsed          time.Time
	LastError         string
	LastErrorTime     time.Time
	mu                sync.RWMutex
}

func (es *EndpointStats) RecordSuccess(responseTime time.Duration) {
	atomic.AddInt64(&es.MessagesSent, 1)
	atomic.AddInt64(&es.TotalResponseTime, responseTime.Milliseconds())
	es.mu.Lock()
	es.LastUsed = time.Now()
	es.mu.Unlock()
}

func (es *EndpointStats) RecordFailure(err string) {
	atomic.AddInt64(&es.MessagesFailed, 1)
	es.mu.Lock()
	es.LastError = err
	es.LastErrorTime = time.Now()
	es.LastUsed = time.Now()
	es.mu.Unlock()
}

func (es *EndpointStats) GetStats() map[string]interface{} {
	es.mu.RLock()
	defer es.mu.RUnlock()

	sent := atomic.LoadInt64(&es.MessagesSent)
	failed := atomic.LoadInt64(&es.MessagesFailed)
	totalTime := atomic.LoadInt64(&es.TotalResponseTime)

	avgResponseTime := int64(0)
	if sent > 0 {
		avgResponseTime = totalTime / sent
	}

	successRate := float64(0)
	total := sent + failed
	if total > 0 {
		successRate = float64(sent) / float64(total) * 100
	}

	return map[string]interface{}{
		"url":                es.URL,
		"messages_sent":      sent,
		"messages_failed":    failed,
		"avg_response_time":  avgResponseTime,
		"success_rate":       successRate,
		"last_used":          es.LastUsed,
		"last_error":         es.LastError,
		"last_error_time":    es.LastErrorTime,
	}
}
