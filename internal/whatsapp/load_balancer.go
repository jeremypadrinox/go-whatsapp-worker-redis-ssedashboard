package whatsapp

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gowhatsapp-worker/internal/models"
	"github.com/sirupsen/logrus"
)

type LoadBalancer struct {
	clients         []*Client
	stats           []*EndpointStats
	circuitBreakers []*CircuitBreaker
	currentIdx      uint64
	logger          *logrus.Logger
	mutex           sync.RWMutex
}

func NewLoadBalancer(endpoints []string, auths []string, logger *logrus.Logger) *LoadBalancer {
	return NewLoadBalancerWithConfig(endpoints, auths, logger, CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      2 * time.Minute,
		HalfOpenTimeout:  30 * time.Second,
	})
}

func NewLoadBalancerWithConfig(endpoints []string, auths []string, logger *logrus.Logger, config CircuitBreakerConfig) *LoadBalancer {
	clients := make([]*Client, len(endpoints))
	stats := make([]*EndpointStats, len(endpoints))
	breakers := make([]*CircuitBreaker, len(endpoints))
	
	for i := range endpoints {
		clients[i] = NewClient(endpoints[i], auths[i], logger)
		stats[i] = &EndpointStats{
			URL: endpoints[i],
		}
		breakers[i] = NewCircuitBreaker(config)
	}

	return &LoadBalancer{
		clients:         clients,
		stats:           stats,
		circuitBreakers: breakers,
		logger:          logger,
	}
}

func (lb *LoadBalancer) SelectClient() *Client {
	idx := atomic.AddUint64(&lb.currentIdx, 1) - 1
	return lb.clients[idx%uint64(len(lb.clients))]
}

// SelectHealthyClient selects only healthy endpoints using circuit breaker
func (lb *LoadBalancer) SelectHealthyClient() (*Client, int) {
	healthyIndices := []int{}
	
	for i := range lb.clients {
		if lb.circuitBreakers[i].CanAttempt() {
			healthyIndices = append(healthyIndices, i)
		}
	}
	
	if len(healthyIndices) == 0 {
		// All endpoints failed, force try first one
		lb.logger.Warn("All endpoints in open state, forcing attempt on first endpoint")
		return lb.clients[0], 0
	}
	
	// Round-robin among healthy endpoints
	idx := atomic.AddUint64(&lb.currentIdx, 1) - 1
	selectedIdx := healthyIndices[idx%uint64(len(healthyIndices))]
	
	return lb.clients[selectedIdx], selectedIdx
}

// isCriticalError determines if error is critical (auth/session)
func (lb *LoadBalancer) isCriticalError(err error, statusCode int) bool {
	if statusCode == 401 || statusCode == 403 {
		return true // Authentication/authorization failures
	}
	
	// Check error message for session/login issues
	if err != nil {
		errMsg := err.Error()
		criticalKeywords := []string{
			"not logged in",
			"session expired",
			"unauthorized",
			"forbidden",
			"qr code",
		}
		for _, keyword := range criticalKeywords {
			if strings.Contains(strings.ToLower(errMsg), keyword) {
				return true
			}
		}
	}
	
	return false
}

func (lb *LoadBalancer) getClientIndex(client *Client) int {
	for i, c := range lb.clients {
		if c == client {
			return i
		}
	}
	return -1
}

func (lb *LoadBalancer) SendMessage(ctx context.Context, target, message string) (*models.WhatsAppResponse, error) {
	start := time.Now()
	client, idx := lb.SelectHealthyClient()
	
	resp, err := client.SendMessage(ctx, target, message)
	
	// Determine status code from response
	statusCode := 200
	if resp != nil && !resp.Success {
		// Extract status code from error message if possible
		if strings.Contains(resp.Error, "HTTP 401") {
			statusCode = 401
		} else if strings.Contains(resp.Error, "HTTP 403") {
			statusCode = 403
		} else if strings.Contains(resp.Error, "HTTP 500") {
			statusCode = 500
		}
	}
	
	if err != nil || (resp != nil && !resp.Success) {
		isCritical := lb.isCriticalError(err, statusCode)
		lb.circuitBreakers[idx].RecordFailure(isCritical)
		lb.stats[idx].RecordFailure(err.Error())
		
		if isCritical {
			lb.logger.WithFields(logrus.Fields{
				"endpoint": lb.clients[idx].GetBaseURL(),
				"error":    err,
				"status":   statusCode,
			}).Warn("Critical endpoint failure detected, opening circuit breaker")
		}
	} else {
		lb.circuitBreakers[idx].RecordSuccess()
		lb.stats[idx].RecordSuccess(time.Since(start))
	}
	
	return resp, err
}

func (lb *LoadBalancer) SendImage(ctx context.Context, target, caption, imagePath string, viewOnce, compress bool, imageURL string) (*models.WhatsAppResponse, error) {
	start := time.Now()
	client, idx := lb.SelectHealthyClient()
	
	resp, err := client.SendImage(ctx, target, caption, imagePath, viewOnce, compress, imageURL)
	
	// Determine status code from response
	statusCode := 200
	if resp != nil && !resp.Success {
		if strings.Contains(resp.Error, "HTTP 401") {
			statusCode = 401
		} else if strings.Contains(resp.Error, "HTTP 403") {
			statusCode = 403
		} else if strings.Contains(resp.Error, "HTTP 500") {
			statusCode = 500
		}
	}
	
	if err != nil || (resp != nil && !resp.Success) {
		isCritical := lb.isCriticalError(err, statusCode)
		lb.circuitBreakers[idx].RecordFailure(isCritical)
		lb.stats[idx].RecordFailure(err.Error())
		
		if isCritical {
			lb.logger.WithFields(logrus.Fields{
				"endpoint": lb.clients[idx].GetBaseURL(),
				"error":    err,
				"status":   statusCode,
			}).Warn("Critical endpoint failure detected, opening circuit breaker")
		}
	} else {
		lb.circuitBreakers[idx].RecordSuccess()
		lb.stats[idx].RecordSuccess(time.Since(start))
	}
	
	return resp, err
}

func (lb *LoadBalancer) SendFile(ctx context.Context, target, caption, filePath string) (*models.WhatsAppResponse, error) {
	start := time.Now()
	client, idx := lb.SelectHealthyClient()
	
	resp, err := client.SendFile(ctx, target, caption, filePath)
	
	// Determine status code from response
	statusCode := 200
	if resp != nil && !resp.Success {
		if strings.Contains(resp.Error, "HTTP 401") {
			statusCode = 401
		} else if strings.Contains(resp.Error, "HTTP 403") {
			statusCode = 403
		} else if strings.Contains(resp.Error, "HTTP 500") {
			statusCode = 500
		}
	}
	
	if err != nil || (resp != nil && !resp.Success) {
		isCritical := lb.isCriticalError(err, statusCode)
		lb.circuitBreakers[idx].RecordFailure(isCritical)
		lb.stats[idx].RecordFailure(err.Error())
		
		if isCritical {
			lb.logger.WithFields(logrus.Fields{
				"endpoint": lb.clients[idx].GetBaseURL(),
				"error":    err,
				"status":   statusCode,
			}).Warn("Critical endpoint failure detected, opening circuit breaker")
		}
	} else {
		lb.circuitBreakers[idx].RecordSuccess()
		lb.stats[idx].RecordSuccess(time.Since(start))
	}
	
	return resp, err
}

func (lb *LoadBalancer) SendTypingPresence(ctx context.Context, target string, isTyping bool) error {
	client, _ := lb.SelectHealthyClient()
	return client.SendTypingPresence(ctx, target, isTyping)
}

func (lb *LoadBalancer) HealthCheck(ctx context.Context) error {
	client, _ := lb.SelectHealthyClient()
	return client.HealthCheck(ctx)
}

func (lb *LoadBalancer) GetEndpoints() []string {
	endpoints := make([]string, len(lb.clients))
	for i, client := range lb.clients {
		endpoints[i] = client.GetBaseURL()
	}
	return endpoints
}

func (lb *LoadBalancer) GetAllStats() []map[string]interface{} {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()
	
	stats := make([]map[string]interface{}, len(lb.stats))
	for i, stat := range lb.stats {
		stats[i] = stat.GetStats()
		// Add circuit breaker status to each endpoint
		stats[i]["circuit_breaker"] = lb.circuitBreakers[i].GetStats()
	}
	return stats
}

// GetCircuitBreakerStats returns circuit breaker stats for a specific endpoint
func (lb *LoadBalancer) GetCircuitBreakerStats(index int) map[string]interface{} {
	if index < 0 || index >= len(lb.circuitBreakers) {
		return nil
	}
	return lb.circuitBreakers[index].GetStats()
}

// GetCircuitBreakerState returns the circuit breaker state for a specific endpoint
func (lb *LoadBalancer) GetCircuitBreakerState(index int) CircuitState {
	if index < 0 || index >= len(lb.circuitBreakers) {
		return StateOpen
	}
	return lb.circuitBreakers[index].GetState()
}

func (lb *LoadBalancer) Close() error {
	for _, client := range lb.clients {
		client.Close()
	}
	return nil
}
