package worker

import (
	"math/rand"
	"time"

	"gowhatsapp-worker/internal/config"
)

type RateLimiter struct {
	config     *config.Config
	rand       *rand.Rand
	delayMin   time.Duration
	delayMax   time.Duration
	typingMin  time.Duration
	typingMax  time.Duration
	breakInterval int
	breakMin   time.Duration
	breakMax   time.Duration
}

func NewRateLimiter(cfg *config.Config) *RateLimiter {
	rl := &RateLimiter{
		config: cfg,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	switch cfg.RateProfile {
	case "bulk":
		rl.delayMin = time.Duration(cfg.RateDelayMeanMs - cfg.RateDelayJitterMs) * time.Millisecond
		rl.delayMax = time.Duration(cfg.RateDelayMeanMs + cfg.RateDelayJitterMs) * time.Millisecond
		rl.typingMin = 3 * time.Second
		rl.typingMax = 8 * time.Second
		rl.breakInterval = cfg.RateBurstLimit
		rl.breakMin = time.Duration(cfg.RateCooldownSeconds) * time.Second
		rl.breakMax = time.Duration(cfg.RateCooldownSeconds + 60) * time.Second
	default:
		rl.delayMin = cfg.RateLimitDelayMin
		rl.delayMax = cfg.RateLimitDelayMax
		rl.typingMin = cfg.TypingDelayMin
		rl.typingMax = cfg.TypingDelayMax
		rl.breakInterval = cfg.NaturalBreakInterval
		rl.breakMin = cfg.NaturalBreakDurationMin
		rl.breakMax = cfg.NaturalBreakDurationMax
	}

	return rl
}

func (rl *RateLimiter) WaitForNextMessage(messageCount int) {
	baseDelay := rl.getRandomDelay(rl.delayMin, rl.delayMax)

	typingDelay := rl.getRandomDelay(rl.typingMin, rl.typingMax)

	totalDelay := baseDelay + typingDelay

	if rl.shouldTakeNaturalBreak(messageCount) {
		breakDuration := rl.getRandomDelay(rl.breakMin, rl.breakMax)
		totalDelay += breakDuration
	}

	time.Sleep(totalDelay)
}

func (rl *RateLimiter) getRandomDelay(min, max time.Duration) time.Duration {
	if min >= max {
		return min
	}

	minNanos := min.Nanoseconds()
	maxNanos := max.Nanoseconds()

	randomNanos := rl.rand.Int63n(maxNanos-minNanos) + minNanos
	return time.Duration(randomNanos)
}

func (rl *RateLimiter) shouldTakeNaturalBreak(messageCount int) bool {
	if rl.breakInterval <= 0 {
		return false
	}

	return messageCount > 0 && messageCount%rl.breakInterval == 0
}

func (rl *RateLimiter) GetTypingDelay() time.Duration {
	return rl.getRandomDelay(rl.typingMin, rl.typingMax)
}

func (rl *RateLimiter) GetMessageDelay() time.Duration {
	return rl.getRandomDelay(rl.delayMin, rl.delayMax)
}

func (rl *RateLimiter) GetNaturalBreakDelay() time.Duration {
	return rl.getRandomDelay(rl.breakMin, rl.breakMax)
}
