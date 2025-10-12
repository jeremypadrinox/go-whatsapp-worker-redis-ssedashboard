package whatsapp

import (
	"testing"
	"time"
)

func TestCircuitBreakerOpenClose(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      100 * time.Millisecond,
		HalfOpenTimeout:  50 * time.Millisecond,
	}
	
	cb := NewCircuitBreaker(config)
	
	// Test initial state
	if cb.GetState() != StateClosed {
		t.Errorf("Expected initial state to be CLOSED, got %v", cb.GetState())
	}
	
	// Test failure threshold
	for i := 0; i < 3; i++ {
		cb.RecordFailure(false)
	}
	
	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be OPEN after 3 failures, got %v", cb.GetState())
	}
	
	// Test that CanAttempt returns false when open
	if cb.CanAttempt() {
		t.Error("Expected CanAttempt to return false when circuit is open")
	}
	
	// Wait for timeout and test half-open state
	time.Sleep(150 * time.Millisecond)
	if !cb.CanAttempt() {
		t.Error("Expected CanAttempt to return true after timeout")
	}
	
	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected state to be HALF_OPEN after timeout, got %v", cb.GetState())
	}
	
	// Test recovery with successes
	for i := 0; i < 2; i++ {
		cb.RecordSuccess()
	}
	
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be CLOSED after 2 successes, got %v", cb.GetState())
	}
}

func TestCircuitBreakerCriticalFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 5, // High threshold
		OpenTimeout:      100 * time.Millisecond,
	}
	
	cb := NewCircuitBreaker(config)
	
	// Test that critical failure opens circuit immediately
	cb.RecordFailure(true)
	
	if cb.GetState() != StateOpen {
		t.Errorf("Expected critical failure to open circuit immediately, got %v", cb.GetState())
	}
	
	if cb.CanAttempt() {
		t.Error("Expected CanAttempt to return false after critical failure")
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      100 * time.Millisecond,
	}
	
	cb := NewCircuitBreaker(config)
	
	// Record some failures and successes
	cb.RecordFailure(false)
	cb.RecordSuccess()
	cb.RecordFailure(false)
	cb.RecordFailure(false) // Should open circuit
	
	stats := cb.GetStats()
	
	if stats["failure_count"] != 3 {
		t.Errorf("Expected failure_count to be 3, got %v", stats["failure_count"])
	}
	
	if stats["success_count"] != 1 {
		t.Errorf("Expected success_count to be 1, got %v", stats["success_count"])
	}
	
	if stats["consecutive_fails"] != 2 {
		t.Errorf("Expected consecutive_fails to be 2, got %v", stats["consecutive_fails"])
	}
	
	if stats["state"] != "open" {
		t.Errorf("Expected state to be 'open', got %v", stats["state"])
	}
}

func TestCircuitBreakerConsecutiveFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenTimeout:      100 * time.Millisecond,
	}
	
	cb := NewCircuitBreaker(config)
	
	// Test that consecutive failures are tracked correctly
	cb.RecordFailure(false)
	cb.RecordSuccess() // This should reset consecutive failures
	cb.RecordFailure(false)
	cb.RecordFailure(false)
	
	// Should still be closed because consecutive failures were reset
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be CLOSED after success reset, got %v", cb.GetState())
	}
	
	// One more failure should open the circuit
	cb.RecordFailure(false)
	
	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be OPEN after 3 consecutive failures, got %v", cb.GetState())
	}
}
