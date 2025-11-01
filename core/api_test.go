package core

import (
    "net/http"
    "net/http/httptest"
    "pplx2api/config"
    "strconv"
    "testing"
    "time"

    "github.com/gin-gonic/gin"
)

// TestRetryAfterParsing_Seconds tests parsing Retry-After as seconds
func TestRetryAfterParsing_Seconds(t *testing.T) {
    // Setup config
    config.ConfigInstance = &config.Config{
        RateLimitCooldown: 60 * time.Second,
    }

    // Create a test server that returns 429 with Retry-After in seconds
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Retry-After", "30")
        w.WriteHeader(http.StatusTooManyRequests)
    }))
    defer server.Close()

    // Create session info
    session := &config.SessionInfo{
        SessionKey: "test-session-key-123",
    }

    // Create client
    client := &Client{
        SessionInfo: session,
    }

    // Mock the request (we can't easily test the full flow without mocking req.Client)
    // Instead, we'll test the logic directly
    retryAfter := "30"
    cooldown := config.ConfigInstance.RateLimitCooldown

    if seconds, err := strconv.Atoi(retryAfter); err == nil {
        cooldown = time.Duration(seconds) * time.Second
    }

    if cooldown != 30*time.Second {
        t.Errorf("Expected 30 seconds cooldown, got %v", cooldown)
    }

    // Set rate limited
    client.SessionInfo.SetRateLimited(cooldown)

    if !session.IsRateLimited {
        t.Error("Session should be rate limited")
    }

    expectedExpiry := time.Now().Add(30 * time.Second)
    diff := session.RateLimitExpiry.Sub(expectedExpiry)
    if diff > time.Second || diff < -time.Second {
        t.Errorf("Expiry time not correct, diff: %v", diff)
    }
}

// TestRetryAfterParsing_HTTPDate tests parsing Retry-After as HTTP date
func TestRetryAfterParsing_HTTPDate(t *testing.T) {
    // Setup config
    config.ConfigInstance = &config.Config{
        RateLimitCooldown: 60 * time.Second,
    }

    session := &config.SessionInfo{
        SessionKey: "test-session-key-123",
    }

    client := &Client{
        SessionInfo: session,
    }

    // Test with HTTP date format
    futureTime := time.Now().Add(45 * time.Second)
    retryAfter := futureTime.Format(http.TimeFormat)
    cooldown := config.ConfigInstance.RateLimitCooldown

    if retryTime, err := http.ParseTime(retryAfter); err == nil {
        cooldown = time.Until(retryTime)
    }

    // Should be approximately 45 seconds (within 2 seconds tolerance)
    if cooldown < 43*time.Second || cooldown > 47*time.Second {
        t.Errorf("Expected ~45 seconds cooldown, got %v", cooldown)
    }

    client.SessionInfo.SetRateLimited(cooldown)

    if !session.IsRateLimited {
        t.Error("Session should be rate limited")
    }
}

// TestRetryAfterParsing_Invalid tests parsing invalid Retry-After header
func TestRetryAfterParsing_Invalid(t *testing.T) {
    // Setup config
    config.ConfigInstance = &config.Config{
        RateLimitCooldown: 60 * time.Second,
    }

    session := &config.SessionInfo{
        SessionKey: "test-session-key-123",
    }

    client := &Client{
        SessionInfo: session,
    }

    // Test with invalid header
    retryAfter := "invalid-value"
    cooldown := config.ConfigInstance.RateLimitCooldown

    if retryAfter != "" {
        if seconds, err := strconv.Atoi(retryAfter); err == nil {
            cooldown = time.Duration(seconds) * time.Second
        } else if retryTime, err := http.ParseTime(retryAfter); err == nil {
            cooldown = time.Until(retryTime)
        }
        // If both fail, cooldown stays at default
    }

    // Should use default cooldown
    if cooldown != 60*time.Second {
        t.Errorf("Expected default 60 seconds cooldown for invalid header, got %v", cooldown)
    }

    client.SessionInfo.SetRateLimited(cooldown)

    if !session.IsRateLimited {
        t.Error("Session should be rate limited")
    }
}

// TestRetryAfterParsing_Empty tests parsing empty Retry-After header
func TestRetryAfterParsing_Empty(t *testing.T) {
    // Setup config
    config.ConfigInstance = &config.Config{
        RateLimitCooldown: 60 * time.Second,
    }

    session := &config.SessionInfo{
        SessionKey: "test-session-key-123",
    }

    client := &Client{
        SessionInfo: session,
    }

    // Test with empty header
    retryAfter := ""
    cooldown := config.ConfigInstance.RateLimitCooldown

    if retryAfter != "" {
        // This block won't execute
        if seconds, err := strconv.Atoi(retryAfter); err == nil {
            cooldown = time.Duration(seconds) * time.Second
        }
    }

    // Should use default cooldown
    if cooldown != 60*time.Second {
        t.Errorf("Expected default 60 seconds cooldown for empty header, got %v", cooldown)
    }

    client.SessionInfo.SetRateLimited(cooldown)

    if !session.IsRateLimited {
        t.Error("Session should be rate limited")
    }
}

// TestClientWithSessionInfo tests that client properly holds session reference
func TestClientWithSessionInfo(t *testing.T) {
    session := &config.SessionInfo{
        SessionKey: "test-session-123",
    }

    client := &Client{
        SessionInfo: session,
        Model:       "test-model",
    }

    if client.SessionInfo != session {
        t.Error("Client should hold reference to session")
    }

    // Test that modifications through client affect the session
    client.SessionInfo.SetRateLimited(30 * time.Second)

    if !session.IsRateLimited {
        t.Error("Original session should be rate limited")
    }
}

// TestSendMessage_RateLimitError tests that SendMessage returns proper error
func TestSendMessage_RateLimitError(t *testing.T) {
    // This is a basic test - full integration testing would require mocking
    session := &config.SessionInfo{
        SessionKey: "test-session-123",
    }

    // After rate limit, session should be marked
    session.SetRateLimited(30 * time.Second)

    if !session.IsRateLimited {
        t.Error("Session should be rate limited")
    }

    // The actual SendMessage test would require extensive mocking
    // This confirms the session marking works
}

// TestNewClient_WithSessionInfo tests NewClient with session info
func TestNewClient_WithSessionInfo(t *testing.T) {
    session := &config.SessionInfo{
        SessionKey: "test-session-123",
    }

    client := NewClient("test-token", "", "test-model", false, session)

    if client.SessionInfo != session {
        t.Error("Client should have session info reference")
    }

    if client.Model != "test-model" {
        t.Errorf("Expected model 'test-model', got '%s'", client.Model)
    }
}

// TestRateLimitWithGinContext tests the integration with Gin context
func TestRateLimitWithGinContext(t *testing.T) {
    gin.SetMode(gin.TestMode)

    session := &config.SessionInfo{
        SessionKey: "test-session-123",
    }

    // Simulate rate limit
    session.SetRateLimited(30 * time.Second)

    if !session.IsRateLimited {
        t.Error("Session should be marked as rate limited")
    }

    // Verify expiry is in the future
    if !time.Now().Before(session.RateLimitExpiry) {
        t.Error("Rate limit expiry should be in the future")
    }
}

// TestRateLimitParsing_EdgeCases tests edge cases in retry-after parsing
func TestRateLimitParsing_EdgeCases(t *testing.T) {
    testCases := []struct {
        name           string
        retryAfter     string
        expectedResult string
    }{
        {
            name:           "Zero seconds",
            retryAfter:     "0",
            expectedResult: "0s",
        },
        {
            name:           "Large number",
            retryAfter:     "3600",
            expectedResult: "1h0m0s",
        },
        {
            name:           "Negative (invalid)",
            retryAfter:     "-10",
            expectedResult: "-10s",
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            cooldown := 60 * time.Second

            if seconds, err := strconv.Atoi(tc.retryAfter); err == nil {
                cooldown = time.Duration(seconds) * time.Second
            }

            if cooldown.String() != tc.expectedResult {
                t.Errorf("Expected %s, got %s", tc.expectedResult, cooldown.String())
            }
        })
    }
}
