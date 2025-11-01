# Rate Limit Handling - Implementation Checklist

This checklist breaks down the recommended improvements into actionable tasks with time estimates and priorities.

---

## Phase 1: Critical Fixes (4-5 hours total)

### Task 1.1: Add Rate Limit Cooldown Structure ⭐⭐⭐
**Priority**: CRITICAL  
**Estimated Time**: 1.5 hours  
**Difficulty**: Easy

- [ ] **Update `SessionInfo` struct** (`config/config.go:16-18`)
  ```go
  type SessionInfo struct {
      SessionKey        string
      IsRateLimited     bool
      RateLimitExpiry   time.Time
      mutex             sync.RWMutex  // For thread-safe access to rate limit fields
  }
  ```

- [ ] **Add helper methods to `SessionInfo`**
  ```go
  func (si *SessionInfo) SetRateLimited(duration time.Duration)
  func (si *SessionInfo) IsAvailable() bool
  func (si *SessionInfo) ClearRateLimit()
  ```

- [ ] **Test thread safety**
  - [ ] Write unit test for concurrent access
  - [ ] Verify no race conditions with `go test -race`

**Files to modify**: `config/config.go`

---

### Task 1.2: Implement Cooldown on 429 Detection ⭐⭐⭐
**Priority**: CRITICAL  
**Estimated Time**: 1.5 hours  
**Difficulty**: Easy

- [ ] **Update `SendMessage` signature** (`core/api.go:158`)
  ```go
  func (c *Client) SendMessage(message string, stream bool, is_incognito bool, 
                                gc *gin.Context, session *config.SessionInfo) (int, error)
  ```

- [ ] **Add cooldown logic on 429** (`core/api.go:221-224`)
  ```go
  if resp.StatusCode == http.StatusTooManyRequests {
      cooldownDuration := 60 * time.Second  // Default
      
      // Parse Retry-After header (Task 1.3)
      if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
          if seconds, err := strconv.Atoi(retryAfter); err == nil {
              cooldownDuration = time.Duration(seconds) * time.Second
          }
      }
      
      session.SetRateLimited(cooldownDuration)
      logger.Info(fmt.Sprintf("Session rate limited until %v", session.RateLimitExpiry))
      
      resp.Body.Close()
      return http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded")
  }
  ```

- [ ] **Update all callers** to pass session pointer
  - [ ] `service/handle.go:151`

- [ ] **Add logging** for rate limit events

**Files to modify**: `core/api.go`, `service/handle.go`

---

### Task 1.3: Parse Retry-After Header ⭐⭐⭐
**Priority**: CRITICAL  
**Estimated Time**: 0.5 hours  
**Difficulty**: Easy

- [ ] **Parse Retry-After header** (already included in Task 1.2)
  - [ ] Handle integer format (seconds)
  - [ ] Handle HTTP-date format (optional)
  - [ ] Fallback to default 60s if parsing fails

- [ ] **Log parsed cooldown duration**

- [ ] **Test with various header values**
  - [ ] `Retry-After: 60`
  - [ ] `Retry-After: 120`
  - [ ] Missing header
  - [ ] Invalid value

**Files to modify**: `core/api.go`

---

### Task 1.4: Skip Rate-Limited Sessions in Retry Loop ⭐⭐⭐
**Priority**: CRITICAL  
**Estimated Time**: 1 hour  
**Difficulty**: Easy

- [ ] **Update retry loop** (`service/handle.go:116-160`)
  ```go
  for i := 0; i < config.ConfigInstance.RetryCount; i++ {
      index = (index + i) % len(config.ConfigInstance.Sessions)
      session, err := config.ConfigInstance.GetSessionForModel(index)
      
      if err != nil {
          logger.Info("Failed to get session, trying next")
          continue
      }
      
      // NEW: Check if session is available
      if !session.IsAvailable() {
          remainingTime := time.Until(session.RateLimitExpiry)
          logger.Info(fmt.Sprintf("Session %d is rate-limited for %v, skipping", 
                                  index, remainingTime))
          continue
      }
      
      // ... rest of retry logic
  }
  ```

- [ ] **Handle case when all sessions are rate-limited**
  - [ ] Return specific error message
  - [ ] Include retry-after time in response
  - [ ] HTTP 429 status to client

- [ ] **Add metrics** for skipped sessions (counter)

**Files to modify**: `service/handle.go`

---

### Task 1.5: Fix Index Increment Bug ⭐
**Priority**: LOW (cleanup)  
**Estimated Time**: 0.5 hours  
**Difficulty**: Easy

- [ ] **Fix double increment** (`service/handle.go:115-121`)
  ```go
  // OLD (buggy):
  index := config.Sr.NextIndex()
  for i := 0; i < config.ConfigInstance.RetryCount; i++ {
      index = (index + 1) % len(config.ConfigInstance.Sessions)  // REDUNDANT
      
  // NEW (fixed):
  startIndex := config.Sr.NextIndex()
  for i := 0; i < config.ConfigInstance.RetryCount; i++ {
      index := (startIndex + i) % len(config.ConfigInstance.Sessions)
  ```

- [ ] **Verify rotation works correctly** after fix

**Files to modify**: `service/handle.go`

---

### Task 1.6: Testing Phase 1 ⭐⭐⭐
**Priority**: CRITICAL  
**Estimated Time**: 2-3 hours  
**Difficulty**: Medium

- [ ] **Unit Tests**
  - [ ] `TestSessionInfo_SetRateLimited`
  - [ ] `TestSessionInfo_IsAvailable`
  - [ ] `TestSessionInfo_ConcurrentAccess` (with -race flag)
  - [ ] `TestRetryAfterParsing`

- [ ] **Integration Tests**
  - [ ] Mock 429 response from Perplexity API
  - [ ] Verify session is skipped during cooldown
  - [ ] Verify session becomes available after expiry
  - [ ] Test with multiple sessions

- [ ] **Manual Testing**
  - [ ] Deploy to test environment
  - [ ] Trigger rate limit on one account
  - [ ] Verify automatic failover
  - [ ] Monitor logs for cooldown messages
  - [ ] Confirm improved error rate

- [ ] **Load Testing**
  - [ ] 100 concurrent requests
  - [ ] Verify no race conditions
  - [ ] Measure retry reduction

**Expected Improvement**: 50-70% reduction in wasted retries

---

## Phase 2: Account Health Tracking (8-10 hours)

### Task 2.1: Expand SessionInfo with Health Metrics ⭐⭐
**Priority**: HIGH  
**Estimated Time**: 2 hours  
**Difficulty**: Medium

- [ ] **Add health tracking fields** (`config/config.go`)
  ```go
  type SessionInfo struct {
      SessionKey        string
      IsRateLimited     bool
      RateLimitExpiry   time.Time
      
      // NEW: Health tracking
      LastUsed          time.Time
      LastError         error
      LastErrorTime     time.Time
      TotalRequests     int
      SuccessCount      int
      ErrorCount        int
      ErrorsByType      map[string]int  // "429", "500", "network", etc.
      
      mutex             sync.RWMutex
  }
  ```

- [ ] **Add update methods**
  ```go
  func (si *SessionInfo) RecordSuccess()
  func (si *SessionInfo) RecordError(errorType string, err error)
  func (si *SessionInfo) GetSuccessRate() float64
  func (si *SessionInfo) GetHealthScore() float64
  ```

- [ ] **Initialize new fields** in LoadConfig

**Files to modify**: `config/config.go`

---

### Task 2.2: Track Request Success/Failure ⭐⭐
**Priority**: HIGH  
**Estimated Time**: 2 hours  
**Difficulty**: Medium

- [ ] **Update session on success** (`core/api.go`)
  ```go
  // After successful response
  session.RecordSuccess()
  ```

- [ ] **Update session on error** (`core/api.go`)
  ```go
  // On 429
  session.RecordError("429", fmt.Errorf("rate limit exceeded"))
  
  // On other errors
  session.RecordError("500", fmt.Errorf("server error"))
  session.RecordError("network", err)
  ```

- [ ] **Categorize errors properly**
  - [ ] 429 → Rate limit
  - [ ] 401/403 → Auth error
  - [ ] 500/502/503 → Server error
  - [ ] Network timeout → Network error
  - [ ] Others → Unknown error

**Files to modify**: `core/api.go`, `service/handle.go`

---

### Task 2.3: Implement Health-Based Selection ⭐⭐
**Priority**: HIGH  
**Estimated Time**: 3 hours  
**Difficulty**: Medium

- [ ] **Create new selection algorithm** (`config/config.go`)
  ```go
  func (c *Config) SelectBestSession() (*SessionInfo, int, error) {
      c.RwMutex.RLock()
      defer c.RwMutex.RUnlock()
      
      var candidates []*sessionCandidate
      
      for i, session := range c.Sessions {
          // Skip rate-limited
          if !session.IsAvailable() {
              continue
          }
          
          // Calculate health score
          score := session.GetHealthScore()
          
          candidates = append(candidates, &sessionCandidate{
              session: &session,
              index:   i,
              score:   score,
          })
      }
      
      if len(candidates) == 0 {
          return nil, -1, fmt.Errorf("no healthy sessions available")
      }
      
      // Sort by score and select best
      // Or use weighted random selection
      best := selectBestCandidate(candidates)
      return best.session, best.index, nil
  }
  ```

- [ ] **Replace round-robin with health-based**
  - [ ] Update `service/handle.go` to use new selection
  - [ ] Keep round-robin as fallback option (config flag)

- [ ] **Choose selection strategy**
  - [ ] Option A: Least recently used
  - [ ] Option B: Highest health score
  - [ ] Option C: Weighted random based on health
  - [ ] Make it configurable via env var

**Files to modify**: `config/config.go`, `service/handle.go`

---

### Task 2.4: Add Basic Logging/Metrics ⭐⭐
**Priority**: MEDIUM  
**Estimated Time**: 2 hours  
**Difficulty**: Easy

- [ ] **Log session health periodically**
  ```go
  func (c *Config) LogSessionHealth() {
      for i, session := range c.Sessions {
          logger.Info(fmt.Sprintf(
              "Session %d: Requests=%d, Success=%.2f%%, Errors=%d, RateLimited=%v",
              i, session.TotalRequests, session.GetSuccessRate()*100,
              session.ErrorCount, session.IsRateLimited))
      }
  }
  ```

- [ ] **Add periodic health logging**
  - [ ] Every 5 minutes or 100 requests
  - [ ] Include in existing job or create new ticker

- [ ] **Log events**
  - [ ] Session selected
  - [ ] Session skipped (with reason)
  - [ ] Session rate-limited
  - [ ] Session recovered

**Files to modify**: `config/config.go`, `job/cookie.go` or new file

---

### Task 2.5: Persist Health State (Optional) ⭐
**Priority**: LOW  
**Estimated Time**: 2 hours  
**Difficulty**: Medium

- [ ] **Update sessions.json format**
  ```json
  {
    "sessions": [
      {
        "session_key": "token1",
        "total_requests": 1234,
        "success_count": 1200,
        "error_count": 34,
        "last_used": "2024-11-01T15:30:00Z"
      }
    ]
  }
  ```

- [ ] **Load state on startup**
- [ ] **Save state periodically**

**Files to modify**: `job/cookie.go`

---

## Phase 3: Advanced Features (15-20 hours)

### Task 3.1: Circuit Breaker Implementation ⭐⭐
**Priority**: MEDIUM  
**Estimated Time**: 5 hours  
**Difficulty**: Hard

- [ ] **Create CircuitBreaker type**
  ```go
  type CircuitBreaker struct {
      MaxFailures    int
      ResetTimeout   time.Duration
      failures       int
      lastFailure    time.Time
      state          CircuitState  // CLOSED, OPEN, HALF_OPEN
      mutex          sync.RWMutex
  }
  ```

- [ ] **Add to SessionInfo**
- [ ] **Implement state machine**
- [ ] **Update selection logic** to check circuit state
- [ ] **Add configuration** for thresholds

**Files to create**: `config/circuit_breaker.go`

---

### Task 3.2: Exponential Backoff ⭐
**Priority**: MEDIUM  
**Estimated Time**: 2 hours  
**Difficulty**: Easy

- [ ] **Add backoff between retries**
  ```go
  backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
  time.Sleep(backoff)
  ```

- [ ] **Configure max backoff**
- [ ] **Make it optional** (env var)

**Files to modify**: `service/handle.go`

---

### Task 3.3: Request Queuing ⭐⭐
**Priority**: LOW  
**Estimated Time**: 6-8 hours  
**Difficulty**: Hard

- [ ] **Create request queue**
- [ ] **Implement rate limiter**
- [ ] **Process queue with backpressure**
- [ ] **Add queue metrics**

**Files to create**: `queue/request_queue.go`

---

### Task 3.4: Prometheus Metrics ⭐⭐
**Priority**: MEDIUM  
**Estimated Time**: 5 hours  
**Difficulty**: Medium

- [ ] **Add prometheus dependency**
  ```bash
  go get github.com/prometheus/client_golang
  ```

- [ ] **Define metrics**
  ```go
  var (
      sessionRequests = prometheus.NewCounterVec(
          prometheus.CounterOpts{
              Name: "pplx_session_requests_total",
              Help: "Total requests per session",
          },
          []string{"session_index"},
      )
      
      sessionErrors = prometheus.NewCounterVec(
          prometheus.CounterOpts{
              Name: "pplx_session_errors_total",
              Help: "Total errors per session",
          },
          []string{"session_index", "error_type"},
      )
      
      rateLimitEvents = prometheus.NewCounter(
          prometheus.CounterOpts{
              Name: "pplx_rate_limit_events_total",
              Help: "Total rate limit events",
          },
      )
  )
  ```

- [ ] **Add /metrics endpoint**
- [ ] **Instrument code**
- [ ] **Create Grafana dashboard** (optional)

**Files to create**: `metrics/prometheus.go`  
**Files to modify**: `router/router.go`

---

### Task 3.5: Alerting (Optional) ⭐
**Priority**: LOW  
**Estimated Time**: 3 hours  
**Difficulty**: Medium

- [ ] **Define alert rules**
  - [ ] All sessions rate-limited
  - [ ] High error rate
  - [ ] Circuit breaker tripped
  - [ ] Session authentication failed

- [ ] **Implement alerting** (webhook, email, etc.)

**Files to create**: `alerting/alerts.go`

---

## Testing & Documentation

### Testing Checklist
- [ ] Unit tests for all new functions
- [ ] Integration tests for retry logic
- [ ] Load tests (100+ concurrent requests)
- [ ] Race condition tests (`go test -race`)
- [ ] Manual testing in staging environment
- [ ] Verify backwards compatibility

### Documentation Checklist
- [ ] Update README.md with new features
- [ ] Add architecture diagrams
- [ ] Document new environment variables
- [ ] Add monitoring guide
- [ ] Create troubleshooting guide
- [ ] Update API documentation

---

## Configuration

### New Environment Variables

Add to `.env.example` and `README.md`:

```env
# Phase 1
RATE_LIMIT_COOLDOWN=60  # seconds, default cooldown

# Phase 2
SESSION_SELECTION_STRATEGY=health  # round-robin|health|lru|random
HEALTH_CHECK_INTERVAL=300  # seconds

# Phase 3
CIRCUIT_BREAKER_ENABLED=true
CIRCUIT_BREAKER_THRESHOLD=5  # failures before trip
CIRCUIT_BREAKER_TIMEOUT=60  # seconds before retry
EXPONENTIAL_BACKOFF_ENABLED=false
ENABLE_METRICS=true
```

---

## Deployment Strategy

### Phase 1 Rollout
1. [ ] Deploy to dev environment
2. [ ] Run smoke tests
3. [ ] Monitor for 24 hours
4. [ ] Deploy to staging
5. [ ] Run load tests
6. [ ] Monitor error rates and logs
7. [ ] Deploy to production (canary)
8. [ ] Gradually increase traffic
9. [ ] Monitor metrics closely

### Rollback Plan
- [ ] Document rollback procedure
- [ ] Keep previous version ready
- [ ] Set up alerts for regressions
- [ ] Define rollback criteria

---

## Success Metrics

### Phase 1 Success Criteria
- [ ] 50-70% reduction in wasted retries
- [ ] Rate-limited sessions properly skipped
- [ ] No increase in error rate
- [ ] No race conditions detected
- [ ] P95 latency improvement

### Phase 2 Success Criteria
- [ ] Health-based selection working
- [ ] Metrics collected and logged
- [ ] Better account utilization (more even distribution)
- [ ] Reduced rate limit events per request

### Phase 3 Success Criteria
- [ ] Circuit breaker prevents cascading failures
- [ ] Prometheus metrics exposed
- [ ] Grafana dashboard operational
- [ ] Alerting functional

---

## Timeline Estimate

| Phase | Duration | Start After |
|-------|----------|-------------|
| Phase 1 | 1 week | Immediately |
| Phase 1 Testing | 3 days | Phase 1 complete |
| Phase 2 | 2 weeks | Phase 1 deployed |
| Phase 2 Testing | 1 week | Phase 2 complete |
| Phase 3 | 3-4 weeks | Phase 2 deployed |
| Phase 3 Testing | 1 week | Phase 3 complete |

**Total**: ~8-10 weeks for complete implementation

**Minimum Viable**: Phase 1 only (1-2 weeks including testing)

---

## Team Assignment (Suggested)

- **Developer 1**: Phase 1 implementation (Tasks 1.1-1.5)
- **Developer 2**: Phase 1 testing (Task 1.6)
- **Developer 1**: Phase 2 implementation (Tasks 2.1-2.3)
- **Developer 2**: Phase 2 metrics/logging (Tasks 2.4-2.5)
- **Developer 1 + 2**: Phase 3 (pair programming for complex features)

---

## Notes

- All tasks marked ⭐⭐⭐ are **critical** for Phase 1
- Tasks marked ⭐⭐ are **recommended** for production use
- Tasks marked ⭐ are **nice-to-have** improvements
- Time estimates assume one developer with moderate Go experience
- Include buffer time for debugging and code review

---

**Last Updated**: 2024-11-01  
**Status**: Ready for implementation
