package whatsapp

import (
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	StateClosed CircuitState = iota // Healthy, accepting requests
	StateOpen                       // Failed, rejecting requests
	StateHalfOpen                   // Testing recovery
)

// CircuitBreaker manages endpoint health and failure handling
type CircuitBreaker struct {
	State            CircuitState
	FailureCount     int
	SuccessCount     int
	LastFailureTime  time.Time
	LastSuccessTime  time.Time
	NextRetryTime    time.Time
	ConsecutiveFails int

	// Thresholds
	FailureThreshold    int           // Failures before opening (default: 3)
	SuccessThreshold    int           // Successes to close from half-open (default: 2)
	OpenTimeout         time.Duration // Time before retry (default: 2 minutes)
	HalfOpenTimeout     time.Duration // Max test time (default: 30 seconds)

	mu sync.RWMutex
}

// Configuration
type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	OpenTimeout      time.Duration
	HalfOpenTimeout  time.Duration
}

func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 3
	}
	if config.SuccessThreshold == 0 {
		config.SuccessThreshold = 2
	}
	if config.OpenTimeout == 0 {
		config.OpenTimeout = 2 * time.Minute
	}
	if config.HalfOpenTimeout == 0 {
		config.HalfOpenTimeout = 30 * time.Second
	}

	return &CircuitBreaker{
		State:            StateClosed,
		FailureThreshold: config.FailureThreshold,
		SuccessThreshold: config.SuccessThreshold,
		OpenTimeout:      config.OpenTimeout,
		HalfOpenTimeout:  config.HalfOpenTimeout,
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.SuccessCount++
	cb.ConsecutiveFails = 0
	cb.LastSuccessTime = time.Now()

	if cb.State == StateHalfOpen {
		if cb.SuccessCount >= cb.SuccessThreshold {
			cb.State = StateClosed
			cb.FailureCount = 0
		}
	}
}

func (cb *CircuitBreaker) RecordFailure(isCritical bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.FailureCount++
	cb.ConsecutiveFails++
	cb.LastFailureTime = time.Now()

	// Critical failures (401, 403) open circuit immediately
	if isCritical {
		cb.State = StateOpen
		cb.NextRetryTime = time.Now().Add(cb.OpenTimeout)
		return
	}

	// Non-critical failures use threshold
	if cb.State == StateClosed && cb.ConsecutiveFails >= cb.FailureThreshold {
		cb.State = StateOpen
		cb.NextRetryTime = time.Now().Add(cb.OpenTimeout)
	} else if cb.State == StateHalfOpen {
		cb.State = StateOpen
		cb.NextRetryTime = time.Now().Add(cb.OpenTimeout)
	}
}

func (cb *CircuitBreaker) CanAttempt() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.State {
	case StateClosed:
		return true
	case StateOpen:
		if time.Now().After(cb.NextRetryTime) {
			cb.State = StateHalfOpen
			cb.SuccessCount = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.State
}

func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stateName := "closed"
	if cb.State == StateOpen {
		stateName = "open"
	} else if cb.State == StateHalfOpen {
		stateName = "half_open"
	}

	return map[string]interface{}{
		"state":              stateName,
		"failure_count":      cb.FailureCount,
		"success_count":      cb.SuccessCount,
		"consecutive_fails":  cb.ConsecutiveFails,
		"last_failure_time":  cb.LastFailureTime,
		"last_success_time":  cb.LastSuccessTime,
		"next_retry_time":    cb.NextRetryTime,
	}
}
