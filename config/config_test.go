package config

import (
	"sync"
	"testing"
	"time"
)

// TestSessionInfo_SetRateLimited tests the SetRateLimited method
func TestSessionInfo_SetRateLimited(t *testing.T) {
	session := &SessionInfo{
		SessionKey: "test-session-key",
	}

	// Initially not rate limited
	if session.IsRateLimited {
		t.Error("Session should not be rate limited initially")
	}

	// Set rate limited with 30 second cooldown
	cooldown := 30 * time.Second
	session.SetRateLimited(cooldown)

	if !session.IsRateLimited {
		t.Error("Session should be rate limited after SetRateLimited")
	}

	// Check that expiry is set correctly (within 1 second tolerance)
	expectedExpiry := time.Now().Add(cooldown)
	diff := session.RateLimitExpiry.Sub(expectedExpiry)
	if diff > time.Second || diff < -time.Second {
		t.Errorf("RateLimitExpiry not set correctly, diff: %v", diff)
	}
}

// TestSessionInfo_IsAvailable tests the IsAvailable method
func TestSessionInfo_IsAvailable(t *testing.T) {
	session := &SessionInfo{
		SessionKey: "test-session-key",
	}

	// Initially available
	if !session.IsAvailable() {
		t.Error("Session should be available initially")
	}

	// Set rate limited with short cooldown
	session.SetRateLimited(100 * time.Millisecond)

	if session.IsAvailable() {
		t.Error("Session should not be available immediately after rate limit")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	if !session.IsAvailable() {
		t.Error("Session should be available after cooldown expires")
	}

	// Check that IsRateLimited was reset
	if session.IsRateLimited {
		t.Error("IsRateLimited should be false after cooldown expires")
	}
}

// TestSessionInfo_ConcurrentAccess tests thread safety
func TestSessionInfo_ConcurrentAccess(t *testing.T) {
	session := &SessionInfo{
		SessionKey: "test-session-key",
	}

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent SetRateLimited calls
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.SetRateLimited(100 * time.Millisecond)
		}()
	}

	// Concurrent IsAvailable calls
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = session.IsAvailable()
		}()
	}

	wg.Wait()

	// If we get here without race detector errors, the test passes
}

// TestSessionInfo_IsAvailable_NoReset tests that IsAvailable doesn't reset if not expired
func TestSessionInfo_IsAvailable_NoReset(t *testing.T) {
	session := &SessionInfo{
		SessionKey: "test-session-key",
	}

	// Set rate limited with long cooldown
	cooldown := 10 * time.Second
	session.SetRateLimited(cooldown)

	// Check multiple times - should stay rate limited
	for i := 0; i < 5; i++ {
		if session.IsAvailable() {
			t.Error("Session should not be available during cooldown")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Should still be rate limited
	if !session.IsRateLimited {
		t.Error("Session should still be rate limited")
	}
}

// TestSessionInfo_MultipleSetRateLimited tests updating cooldown
func TestSessionInfo_MultipleSetRateLimited(t *testing.T) {
	session := &SessionInfo{
		SessionKey: "test-session-key",
	}

	// Set first cooldown
	session.SetRateLimited(1 * time.Second)
	firstExpiry := session.RateLimitExpiry

	time.Sleep(10 * time.Millisecond)

	// Set second cooldown (should update)
	session.SetRateLimited(2 * time.Second)
	secondExpiry := session.RateLimitExpiry

	if !secondExpiry.After(firstExpiry) {
		t.Error("Second expiry should be after first expiry")
	}
}

// TestGetSessionForModel tests GetSessionForModel returns pointer
func TestGetSessionForModel(t *testing.T) {
	config := &Config{
		Sessions: []SessionInfo{
			{SessionKey: "session1"},
			{SessionKey: "session2"},
		},
		RwMutex: sync.RWMutex{},
	}

	session1, err := config.GetSessionForModel(0)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if session1.SessionKey != "session1" {
		t.Errorf("Expected session1, got %s", session1.SessionKey)
	}

	// Test that modifying the returned pointer affects the original
	session1.SetRateLimited(1 * time.Second)

	// Get again and check rate limit is still set
	session1Again, err := config.GetSessionForModel(0)
	if err != nil {
		t.Fatalf("Failed to get session again: %v", err)
	}

	if !session1Again.IsRateLimited {
		t.Error("Session should still be rate limited (pointer should be same)")
	}
}

// TestGetSessionForModel_InvalidIndex tests error handling
func TestGetSessionForModel_InvalidIndex(t *testing.T) {
	config := &Config{
		Sessions: []SessionInfo{
			{SessionKey: "session1"},
		},
		RwMutex: sync.RWMutex{},
	}

	// Test negative index
	_, err := config.GetSessionForModel(-1)
	if err == nil {
		t.Error("Expected error for negative index")
	}

	// Test out of bounds index
	_, err = config.GetSessionForModel(10)
	if err == nil {
		t.Error("Expected error for out of bounds index")
	}

	// Test empty sessions
	emptyConfig := &Config{
		Sessions: []SessionInfo{},
		RwMutex:  sync.RWMutex{},
	}
	_, err = emptyConfig.GetSessionForModel(0)
	if err == nil {
		t.Error("Expected error for empty sessions")
	}
}
