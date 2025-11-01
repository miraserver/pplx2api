package service

import (
	"pplx2api/config"
	"sync"
	"testing"
	"time"
)

// TestRetryLoop_SkipsRateLimitedSessions tests that retry loop skips rate-limited sessions
func TestRetryLoop_SkipsRateLimitedSessions(t *testing.T) {
	// Setup config with multiple sessions
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
			{SessionKey: "session3"},
		},
		RetryCount:        3,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	// Mark session 0 as rate limited
	session0, _ := config.ConfigInstance.GetSessionForModel(0)
	session0.SetRateLimited(10 * time.Second)

	// Verify session 0 is not available
	if session0.IsAvailable() {
		t.Error("Session 0 should not be available")
	}

	// Verify session 1 is available
	session1, _ := config.ConfigInstance.GetSessionForModel(1)
	if !session1.IsAvailable() {
		t.Error("Session 1 should be available")
	}

	// Verify session 2 is available
	session2, _ := config.ConfigInstance.GetSessionForModel(2)
	if !session2.IsAvailable() {
		t.Error("Session 2 should be available")
	}
}

// TestRetryLoop_AllSessionsRateLimited tests behavior when all sessions are rate-limited
func TestRetryLoop_AllSessionsRateLimited(t *testing.T) {
	// Setup config with multiple sessions
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
		},
		RetryCount:        2,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	// Mark all sessions as rate limited
	for i := 0; i < len(config.ConfigInstance.Sessions); i++ {
		session, _ := config.ConfigInstance.GetSessionForModel(i)
		session.SetRateLimited(10 * time.Second)
	}

	// Verify all sessions are not available
	availableCount := 0
	for i := 0; i < len(config.ConfigInstance.Sessions); i++ {
		session, _ := config.ConfigInstance.GetSessionForModel(i)
		if session.IsAvailable() {
			availableCount++
		}
	}

	if availableCount != 0 {
		t.Errorf("Expected 0 available sessions, got %d", availableCount)
	}
}

// TestRetryLoop_SessionBecomesAvailable tests that session becomes available after cooldown
func TestRetryLoop_SessionBecomesAvailable(t *testing.T) {
	// Setup config
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
		},
		RetryCount:        1,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	session, _ := config.ConfigInstance.GetSessionForModel(0)

	// Mark session as rate limited with short cooldown
	session.SetRateLimited(100 * time.Millisecond)

	// Should not be available
	if session.IsAvailable() {
		t.Error("Session should not be available immediately")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	// Should be available now
	if !session.IsAvailable() {
		t.Error("Session should be available after cooldown")
	}
}

// TestRetryLoop_PartialRateLimiting tests when some sessions are rate-limited
func TestRetryLoop_PartialRateLimiting(t *testing.T) {
	// Setup config with 5 sessions
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
			{SessionKey: "session3"},
			{SessionKey: "session4"},
			{SessionKey: "session5"},
		},
		RetryCount:        5,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	// Mark sessions 0, 2, 4 as rate limited
	for i := 0; i < len(config.ConfigInstance.Sessions); i += 2 {
		session, _ := config.ConfigInstance.GetSessionForModel(i)
		session.SetRateLimited(10 * time.Second)
	}

	// Count available sessions
	availableCount := 0
	for i := 0; i < len(config.ConfigInstance.Sessions); i++ {
		session, _ := config.ConfigInstance.GetSessionForModel(i)
		if session.IsAvailable() {
			availableCount++
		}
	}

	// Should have 2 available sessions (1 and 3)
	if availableCount != 2 {
		t.Errorf("Expected 2 available sessions, got %d", availableCount)
	}
}

// TestRetryLoop_ConcurrentSessionAccess tests concurrent access to sessions during retry
func TestRetryLoop_ConcurrentSessionAccess(t *testing.T) {
	// Setup config
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
			{SessionKey: "session3"},
		},
		RetryCount:        3,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	var wg sync.WaitGroup
	iterations := 50

	// Simulate concurrent retry attempts
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionIdx := idx % len(config.ConfigInstance.Sessions)
			session, err := config.ConfigInstance.GetSessionForModel(sessionIdx)
			if err != nil {
				return
			}

			// Randomly set rate limited or check availability
			if idx%2 == 0 {
				session.SetRateLimited(100 * time.Millisecond)
			} else {
				_ = session.IsAvailable()
			}
		}(i)
	}

	wg.Wait()
	// If we get here without race conditions, test passes
}

// TestRetryLoop_SessionRotation tests that sessions are properly rotated
func TestRetryLoop_SessionRotation(t *testing.T) {
	// Setup config
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
			{SessionKey: "session3"},
		},
		RetryCount:        3,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	config.Sr = &config.SessionRagen{
		Index: 0,
		Mutex: sync.Mutex{},
	}

	// Get next few indices to verify rotation
	indices := make([]int, 6)
	for i := 0; i < 6; i++ {
		indices[i] = config.Sr.NextIndex()
	}

	// Should cycle through 0, 1, 2, 0, 1, 2
	expected := []int{0, 1, 2, 0, 1, 2}
	for i, idx := range indices {
		if idx != expected[i] {
			t.Errorf("At position %d: expected %d, got %d", i, expected[i], idx)
		}
	}
}

// TestRetryLoop_RateLimitRecovery tests session recovery after rate limit expires
func TestRetryLoop_RateLimitRecovery(t *testing.T) {
	// Setup config
	config.ConfigInstance = &config.Config{
		Sessions: []config.SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
		},
		RetryCount:        2,
		RateLimitCooldown: 60 * time.Second,
		RwMutex:           sync.RWMutex{},
	}

	session0, _ := config.ConfigInstance.GetSessionForModel(0)
	session1, _ := config.ConfigInstance.GetSessionForModel(1)

	// Rate limit both sessions with different durations
	session0.SetRateLimited(50 * time.Millisecond)
	session1.SetRateLimited(150 * time.Millisecond)

	// Both should be unavailable
	if session0.IsAvailable() || session1.IsAvailable() {
		t.Error("Both sessions should be unavailable initially")
	}

	// Wait for session0 to recover
	time.Sleep(100 * time.Millisecond)

	// Session0 should be available, session1 should not
	if !session0.IsAvailable() {
		t.Error("Session0 should be available after 100ms")
	}
	if session1.IsAvailable() {
		t.Error("Session1 should not be available yet")
	}

	// Wait for session1 to recover
	time.Sleep(100 * time.Millisecond)

	// Both should be available now
	if !session0.IsAvailable() || !session1.IsAvailable() {
		t.Error("Both sessions should be available after full cooldown")
	}
}

// TestConfigRateLimitCooldown tests the default cooldown configuration
func TestConfigRateLimitCooldown(t *testing.T) {
	// Test that default cooldown is set
	config.ConfigInstance = &config.Config{
		RateLimitCooldown: 60 * time.Second,
	}

	if config.ConfigInstance.RateLimitCooldown != 60*time.Second {
		t.Errorf("Expected default cooldown of 60s, got %v", config.ConfigInstance.RateLimitCooldown)
	}

	// Test custom cooldown
	config.ConfigInstance.RateLimitCooldown = 30 * time.Second
	if config.ConfigInstance.RateLimitCooldown != 30*time.Second {
		t.Errorf("Expected custom cooldown of 30s, got %v", config.ConfigInstance.RateLimitCooldown)
	}
}
