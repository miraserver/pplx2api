# Rate Limit Handling & Account Rotation Analysis

**Date**: 2024-11-01  
**Codebase**: pplx2api (Go 1.22+ Perplexity API wrapper)

---

## Executive Summary

The system implements **basic multi-account rotation** with **automatic retry on failures**, but lacks **sophisticated rate limit handling**. While 429 errors are detected, there is no account-level health tracking, cooldown periods, or intelligent selection based on account availability.

### Quick Assessment

| Feature | Status | Notes |
|---------|--------|-------|
| Multi-account support | ✅ Implemented | Comma-separated session tokens |
| Account rotation | ✅ Implemented | Simple round-robin |
| 429 error detection | ✅ Implemented | Explicit check in api.go |
| Automatic retry | ✅ Implemented | Retries all sessions on error |
| Rate limit specific handling | ❌ Missing | Treats 429 like any error |
| Per-account state tracking | ❌ Missing | No usage/health tracking |
| Cooldown periods | ❌ Missing | Failed accounts retried immediately |
| Retry-After header parsing | ❌ Missing | Doesn't respect server hints |

---

## 1. Configuration & Token Management

### Location
- **Primary**: `config/config.go`
- **Environment**: `.env` via `SESSIONS` variable

### Token Loading Mechanism

**Code Reference**: `config/config.go:43-62`

```go
func parseSessionEnv(envValue string) (int, []SessionInfo) {
    if envValue == "" {
        return 0, []SessionInfo{}
    }
    var sessions []SessionInfo
    sessionPairs := strings.Split(envValue, ",")
    retryCount := len(sessionPairs)
    for _, pair := range sessionPairs {
        if pair == "" {
            retryCount--
            continue
        }
        parts := strings.Split(pair, ":")
        session := SessionInfo{
            SessionKey: parts[0],
        }
        sessions = append(sessions, session)
    }
    return retryCount, sessions
}
```

**How it works**:
1. Reads `SESSIONS` environment variable
2. Splits by comma (`,`) to get individual session tokens
3. Each token is a `__Secure-next-auth.session-token` cookie value from perplexity.ai
4. Stores in `[]SessionInfo` slice
5. Sets `RetryCount` = number of sessions

**Example Configuration**:
```env
SESSIONS=eyJhbGciOiJkaXIiLCJlbmMiOiJBMjU2R0NNIn0**,eyJhbGciOiJkaXIiLCJlbmMiOiJBMjU2R0NNIn1**
```

### Data Structures

**Code Reference**: `config/config.go:16-40`

```go
type SessionInfo struct {
    SessionKey string
}

type SessionRagen struct {
    Index int
    Mutex sync.Mutex
}

type Config struct {
    Sessions               []SessionInfo
    // ... other fields
    RwMutex                sync.RWMutex
    // ...
}
```

**Key Points**:
- `SessionInfo`: Simple wrapper around session key string
- **No state tracking**: No usage counters, error counts, or health status
- `SessionRagen`: Manages rotation index with mutex for thread safety
- `Config.RwMutex`: Protects concurrent access to session list

### Thread Safety

✅ **Properly implemented**:
- Read lock when accessing sessions: `config/config.go:69-71`
- Write lock when updating sessions: `job/cookie.go:202-204`
- Mutex protection for rotation index: `config/config.go:127-133`

---

## 2. Rotation Logic

### A. Request-Based Rotation

**Location**: `config/config.go:127-134`

```go
func (sr *SessionRagen) NextIndex() int {
    sr.Mutex.Lock()
    defer sr.Mutex.Unlock()
    
    index := sr.Index
    sr.Index = (index + 1) % len(ConfigInstance.Sessions)
    return index
}
```

**Algorithm**: Simple **round-robin**
- Thread-safe increment using mutex
- Modulo operation ensures wraparound
- Returns OLD index, then increments for next call
- No consideration for account health/availability

**Usage in Requests**: `service/handle.go:115-121`

```go
index := config.Sr.NextIndex()
for i := 0; i < config.ConfigInstance.RetryCount; i++ {
    if i > 0 {
        prompt.Reset()
        prompt.WriteString(rootPrompt.String())
    }
    index = (index + 1) % len(config.ConfigInstance.Sessions)
    session, err := config.ConfigInstance.GetSessionForModel(index)
    // ... use session
}
```

⚠️ **Issue**: Index is incremented AGAIN in the loop, meaning first call uses `NextIndex()+1`, not the returned value.

### B. Background Cookie Refresh Job

**Location**: `job/cookie.go`

**Initialization**: `main.go:19-22`

```go
sessionUpdater := job.GetSessionUpdater(24 * time.Hour)
sessionUpdater.Start()
defer sessionUpdater.Stop()
```

**Key Features**:

1. **Time-Based Refresh**: Every 24 hours
   - `job/cookie.go:146-158` - Timer loop
   
2. **Cookie Renewal Process**: `job/cookie.go:162-211`
   ```go
   func (su *SessionUpdater) updateAllSessions() {
       // Copy current sessions
       // For each session:
       //   - Call client.GetNewCookie()
       //   - Update session token
       // Replace all sessions atomically
       // Save to sessions.json
   }
   ```

3. **Parallel Updates**: Uses `sync.WaitGroup` to update all sessions concurrently

4. **Persistence**: `job/cookie.go:88-114`
   - Saves to `sessions.json` file
   - Loads on startup: `job/cookie.go:59-86`
   - Allows sessions to survive restarts

**GetNewCookie Implementation**: `core/api.go:602-618`

```go
func (c *Client) GetNewCookie() (string, error) {
    resp, err := c.client.R().Get("https://www.perplexity.ai/api/auth/session")
    if err != nil {
        return "", err
    }
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }
    for _, cookie := range resp.Cookies() {
        if cookie.Name == "__Secure-next-auth.session-token" {
            return cookie.Value, nil
        }
    }
    return "", fmt.Errorf("session cookie not found")
}
```

**Rotation Triggers Summary**:

| Trigger Type | Implemented | Details |
|--------------|-------------|---------|
| Request-based | ✅ Yes | Round-robin on each request |
| Time-based (cookie refresh) | ✅ Yes | 24-hour interval |
| Error-based (failover) | ⚠️ Partial | Retries on error but no cooldown |
| Usage-based | ❌ No | No quota tracking |
| Load-based | ❌ No | No account health metrics |

---

## 3. Rate Limit Detection

### HTTP 429 Handling

**Location**: `core/api.go:221-224`

```go
if resp.StatusCode == http.StatusTooManyRequests {
    resp.Body.Close()
    return http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded")
}
```

✅ **Detected**: System explicitly checks for 429 status code  
❌ **Not Special**: Treated same as any other error in retry logic

### Error Propagation Flow

```
core/api.go:SendMessage()
    └─> Returns (statusCode, error)
         └─> service/handle.go:151
              └─> Logs "Failed to send message"
                   └─> Continues to next session (line 155)
```

### What's Missing

❌ **No Header Parsing**:
- Perplexity may return `Retry-After` header
- System doesn't check or respect it
- May spam rate-limited endpoints

❌ **No Perplexity-Specific Error Messages**:
- Doesn't parse response body for rate limit details
- Missing potential error codes/messages from API

❌ **No Quota Tracking**:
- No proactive monitoring of usage
- No "remaining requests" counter
- Can't predict rate limits

❌ **No Differentiation**:
- Network errors, auth errors, and rate limits all trigger same retry
- Could optimize by NOT retrying auth failures

---

## 4. Automatic Failover

### Retry Mechanism

**Location**: `service/handle.go:113-164`

```go
index := config.Sr.NextIndex()
for i := 0; i < config.ConfigInstance.RetryCount; i++ {
    if i > 0 {
        prompt.Reset()
        prompt.WriteString(rootPrompt.String())
    }
    index = (index + 1) % len(config.ConfigInstance.Sessions)
    session, err := config.ConfigInstance.GetSessionForModel(index)
    
    if err != nil {
        logger.Info("Retrying another session")
        continue
    }
    
    pplxClient = core.NewClient(session.SessionKey, ...)
    
    // Try image upload
    if len(img_data_list) > 0 {
        err := pplxClient.UploadImage(img_data_list)
        if err != nil {
            logger.Info("Retrying another session")
            continue
        }
    }
    
    // Try text upload
    if prompt.Len() > config.MaxChatHistoryLength {
        err := pplxClient.UploadText(prompt.String())
        if err != nil {
            logger.Info("Retrying another session")
            continue
        }
    }
    
    // Send message
    if _, err := pplxClient.SendMessage(...); err != nil {
        logger.Info("Retrying another session")
        continue
    }
    
    return // Success
}

// All retries failed
c.JSON(http.StatusInternalServerError, ...)
```

### How Failover Works

✅ **Automatic Switching**: Yes, on ANY error (not just rate limits)

**Error Types That Trigger Retry**:
1. Session retrieval failure
2. Image upload failure  
3. Text upload failure
4. Message send failure (including 429)

**Retry Strategy**:
- Max retries = number of sessions
- Tries each session once in round-robin order
- No exponential backoff
- No delay between retries

### Problems with Current Approach

❌ **No Cooldown Period**:
- Rate-limited account can be retried on very next request
- If high request rate, same account may be selected before cooldown expires

❌ **No Account State**:
- Doesn't mark accounts as "rate-limited"
- No temporary disabling of failed accounts
- No health tracking

❌ **Inefficient Retry Pattern**:
```
Request 1: Account A (429) → Account B (429) → Account C (success)
Request 2: Account A (429 again!) → Account B (429 again!) → Account C (success)
Request 3: Account A (429 again!) → ...
```

❌ **No Error Categorization**:
- Permanent failures (auth errors) treated same as temporary (rate limits)
- Could skip certain accounts entirely on permanent failures

---

## 5. Current Gaps & Improvement Opportunities

### Critical Gaps

#### 1. No Account Health Tracking

**Current State**: Zero state per account  
**Needed**:
```go
type SessionInfo struct {
    SessionKey      string
    LastUsed        time.Time
    ErrorCount      int
    LastError       error
    LastErrorTime   time.Time
    IsRateLimited   bool
    RateLimitExpiry time.Time
    RequestCount    int
    SuccessCount    int
}
```

**Benefits**:
- Skip rate-limited accounts until cooldown expires
- Prioritize healthy accounts
- Identify problematic accounts
- Metrics and observability

#### 2. No Intelligent Selection

**Current**: Simple round-robin  
**Better Options**:
- **Least-recently-used**: Spread load evenly
- **Health-based**: Skip unhealthy accounts
- **Weighted random**: Avoid patterns detectable by API
- **Least-used**: Balance usage across accounts

**Example Algorithm**:
```go
func (c *Config) SelectBestSession() (*SessionInfo, int, error) {
    c.RwMutex.RLock()
    defer c.RwMutex.RUnlock()
    
    var bestSession *SessionInfo
    var bestIndex int
    
    for i, session := range c.Sessions {
        // Skip rate-limited accounts
        if session.IsRateLimited && time.Now().Before(session.RateLimitExpiry) {
            continue
        }
        
        // Select least recently used healthy account
        if bestSession == nil || session.LastUsed.Before(bestSession.LastUsed) {
            bestSession = &session
            bestIndex = i
        }
    }
    
    if bestSession == nil {
        return nil, -1, fmt.Errorf("no healthy sessions available")
    }
    
    return bestSession, bestIndex, nil
}
```

#### 3. No Retry-After Header Respect

**Current**: Ignores all rate limit headers  
**Needed**:
```go
if resp.StatusCode == http.StatusTooManyRequests {
    retryAfter := resp.Header.Get("Retry-After")
    if retryAfter != "" {
        // Parse and store cooldown time
        if seconds, err := strconv.Atoi(retryAfter); err == nil {
            session.RateLimitExpiry = time.Now().Add(time.Duration(seconds) * time.Second)
            session.IsRateLimited = true
        }
    }
    return http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded")
}
```

#### 4. No Circuit Breaker Pattern

**Concept**: Automatically "trip" an account after repeated failures

```go
type CircuitBreaker struct {
    MaxFailures    int
    ResetTimeout   time.Duration
    failures       int
    lastFailure    time.Time
    state          string // "closed", "open", "half-open"
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.failures = 0
    cb.state = "closed"
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.failures++
    cb.lastFailure = time.Now()
    if cb.failures >= cb.MaxFailures {
        cb.state = "open" // Stop using this account
    }
}

func (cb *CircuitBreaker) CanAttempt() bool {
    if cb.state == "open" {
        if time.Since(cb.lastFailure) > cb.ResetTimeout {
            cb.state = "half-open" // Try once
            return true
        }
        return false
    }
    return true
}
```

#### 5. No Usage Quota Monitoring

**Perplexity Likely Has**:
- Requests per hour/day limits
- Token usage limits
- Model-specific limits

**System Should Track**:
```go
type QuotaTracker struct {
    RequestsPerHour   int
    RequestsThisHour  int
    HourStartTime     time.Time
    TotalRequests     int
}

func (qt *QuotaTracker) CanMakeRequest() bool {
    qt.ResetIfNeeded()
    return qt.RequestsThisHour < qt.RequestsPerHour
}

func (qt *QuotaTracker) RecordRequest() {
    qt.ResetIfNeeded()
    qt.RequestsThisHour++
    qt.TotalRequests++
}
```

### Medium Priority Gaps

#### 6. Limited Observability

**Current**: Only basic error logging  
**Needed**:
- Prometheus metrics for account usage
- Per-account success/failure rates
- Rate limit event alerting
- Request latency tracking

#### 7. No Exponential Backoff

**Current**: Immediate retry  
**Better**:
```go
func (c *Client) SendMessageWithBackoff(message string, ...) error {
    maxRetries := 3
    for i := 0; i < maxRetries; i++ {
        err := c.SendMessage(message, ...)
        if err == nil {
            return nil
        }
        
        if i < maxRetries-1 {
            backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
            time.Sleep(backoff)
        }
    }
    return fmt.Errorf("failed after %d retries", maxRetries)
}
```

#### 8. No Request Queuing

**Problem**: Under high load, all requests may exhaust all accounts  
**Solution**: Queue requests and rate-limit internally

```go
type RequestQueue struct {
    queue     chan *Request
    rateLimit int // requests per second
}

func (rq *RequestQueue) Submit(req *Request) {
    rq.queue <- req
}

func (rq *RequestQueue) ProcessQueue() {
    ticker := time.NewTicker(time.Second / time.Duration(rq.rateLimit))
    for {
        select {
        case req := <-rq.queue:
            go req.Process()
        case <-ticker.C:
            // Rate limiting tick
        }
    }
}
```

### Low Priority Enhancements

#### 9. Account Priority/Weight System

Allow configuration of account priorities:
```env
SESSIONS=token1:priority=high,token2:priority=low
```

#### 10. Burst Allowance

Track and allow burst usage within limits:
```go
type BurstTracker struct {
    BurstSize        int
    RefillRate       int // per second
    CurrentTokens    int
    LastRefillTime   time.Time
}
```

---

## 6. Code Reference Summary

### Key Files and Their Roles

| File | Purpose | Key Functions |
|------|---------|---------------|
| `config/config.go` | Token storage & rotation | `LoadConfig()`, `GetSessionForModel()`, `NextIndex()` |
| `job/cookie.go` | Background cookie refresh | `updateAllSessions()`, `GetSessionUpdater()` |
| `core/api.go` | API client & 429 detection | `SendMessage()`, `GetNewCookie()` |
| `service/handle.go` | Request handling & retry | `ChatCompletionsHandler()` |
| `main.go` | App initialization | Starts cookie updater job |

### Critical Code Locations

**Rate Limit Detection**:
- Line: `core/api.go:221-224`
- Returns: `(http.StatusTooManyRequests, error)`

**Retry Loop**:
- Line: `service/handle.go:116-160`
- Retries: Up to `config.RetryCount` times

**Round-Robin Rotation**:
- Line: `config/config.go:127-134`
- Algorithm: `(index + 1) % len(Sessions)`

**Session Refresh Job**:
- Line: `job/cookie.go:162-211`
- Interval: 24 hours (set in `main.go:19`)

**Session Persistence**:
- File: `sessions.json` (created at runtime)
- Save: `job/cookie.go:88-114`
- Load: `job/cookie.go:59-86`

---

## 7. Recommended Implementation Priorities

### Phase 1: Critical Fixes (High Impact, Low Effort)

1. **Add Rate Limit Cooldown** (2-3 hours)
   - Add `IsRateLimited bool` and `RateLimitExpiry time.Time` to `SessionInfo`
   - Set cooldown on 429 errors (default 60 seconds)
   - Skip rate-limited accounts in selection

2. **Parse Retry-After Header** (1 hour)
   - Extract `Retry-After` from 429 responses
   - Use server-specified cooldown instead of fixed value

3. **Fix Index Increment Bug** (30 min)
   - Line `service/handle.go:121` increments index unnecessarily
   - Should use value returned from `NextIndex()` directly

### Phase 2: Account Health Tracking (Medium Impact, Medium Effort)

4. **Expand SessionInfo Structure** (3-4 hours)
   - Add error tracking fields
   - Add usage statistics
   - Add health status

5. **Implement Health-Based Selection** (4-5 hours)
   - Replace round-robin with least-recently-used + health check
   - Skip unhealthy accounts automatically

6. **Add Basic Metrics** (3-4 hours)
   - Count per-account requests, errors, rate limits
   - Log metrics periodically
   - Optional: Expose Prometheus endpoint

### Phase 3: Advanced Features (High Impact, High Effort)

7. **Circuit Breaker Implementation** (5-6 hours)
   - Add circuit breaker per account
   - Automatically disable failing accounts
   - Auto-recover after timeout

8. **Exponential Backoff** (2-3 hours)
   - Add retry delay with exponential increase
   - Configurable max retries and max delay

9. **Request Queuing & Internal Rate Limiting** (8-10 hours)
   - Implement request queue
   - Add per-account rate limiting
   - Prevent exhausting all accounts simultaneously

10. **Observability Dashboard** (10-15 hours)
    - Prometheus metrics export
    - Grafana dashboard
    - Alerting on rate limit events

---

## 8. Example Implementation: Rate Limit Cooldown

Here's a minimal implementation for Phase 1 #1:

### Step 1: Update SessionInfo Structure

```go
// config/config.go
type SessionInfo struct {
    SessionKey        string
    IsRateLimited     bool
    RateLimitExpiry   time.Time
    mutex             sync.RWMutex
}

func (si *SessionInfo) SetRateLimited(duration time.Duration) {
    si.mutex.Lock()
    defer si.mutex.Unlock()
    si.IsRateLimited = true
    si.RateLimitExpiry = time.Now().Add(duration)
}

func (si *SessionInfo) IsAvailable() bool {
    si.mutex.RLock()
    defer si.mutex.RUnlock()
    if si.IsRateLimited {
        if time.Now().After(si.RateLimitExpiry) {
            si.mutex.RUnlock()
            si.mutex.Lock()
            si.IsRateLimited = false
            si.mutex.Unlock()
            si.mutex.RLock()
        }
        return !si.IsRateLimited
    }
    return true
}
```

### Step 2: Update SendMessage to Set Cooldown

```go
// core/api.go
func (c *Client) SendMessage(message string, stream bool, is_incognito bool, gc *gin.Context, session *config.SessionInfo) (int, error) {
    // ... existing code ...
    
    if resp.StatusCode == http.StatusTooManyRequests {
        resp.Body.Close()
        
        // Set cooldown for this session
        cooldownDuration := 60 * time.Second
        
        // Check for Retry-After header
        if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
            if seconds, err := strconv.Atoi(retryAfter); err == nil {
                cooldownDuration = time.Duration(seconds) * time.Second
            }
        }
        
        session.SetRateLimited(cooldownDuration)
        logger.Info(fmt.Sprintf("Session rate limited, cooldown until %v", session.RateLimitExpiry))
        
        return http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded")
    }
    
    // ... rest of function ...
}
```

### Step 3: Update Handler to Check Availability

```go
// service/handle.go
func ChatCompletionsHandler(c *gin.Context) {
    // ... existing code ...
    
    index := config.Sr.NextIndex()
    for i := 0; i < config.ConfigInstance.RetryCount; i++ {
        index = (index + i) % len(config.ConfigInstance.Sessions)
        
        session, err := config.ConfigInstance.GetSessionForModel(index)
        if err != nil {
            logger.Info("Failed to get session, trying next")
            continue
        }
        
        // Check if session is available (not rate-limited)
        if !session.IsAvailable() {
            logger.Info(fmt.Sprintf("Session %d is rate-limited, skipping", index))
            continue
        }
        
        logger.Info(fmt.Sprintf("Using session for model %s: %s", model, session.SessionKey))
        
        pplxClient = core.NewClient(session.SessionKey, config.ConfigInstance.Proxy, model, openSearch)
        
        // ... rest of retry logic ...
        
        if statusCode, err := pplxClient.SendMessage(prompt.String(), req.Stream, config.ConfigInstance.IsIncognito, c, &session); err != nil {
            logger.Error(fmt.Sprintf("Failed to send message (status %d): %v", statusCode, err))
            logger.Info("Retrying another session")
            continue
        }
        
        return
    }
    
    // ... error response ...
}
```

---

## 9. Testing Recommendations

### Test Scenarios for Rate Limit Handling

1. **Single Account Rate Limit**
   - Trigger 429 on account A
   - Verify system switches to account B
   - Verify account A is skipped for cooldown period
   - Verify account A becomes available after cooldown

2. **All Accounts Rate Limited**
   - Trigger 429 on all accounts
   - Verify graceful error message
   - Verify system retries after cooldown

3. **Concurrent Requests**
   - Send 100 requests simultaneously
   - Verify accounts are rotated properly
   - Verify no race conditions
   - Verify all requests complete

4. **Cookie Refresh During Rate Limit**
   - Rate limit account A
   - Trigger cookie refresh job
   - Verify rate limit state persists

5. **Retry-After Header Parsing**
   - Mock 429 response with various Retry-After values
   - Verify correct cooldown duration is used

### Load Testing

```bash
# Use artillery or similar tool
artillery quick --count 100 --num 10 http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer test" \
  -d '{"model":"claude-3.7-sonnet","messages":[{"role":"user","content":"test"}]}'
```

---

## 10. Conclusion

### Current State: ⚠️ **Functional but Not Robust**

The system has a solid foundation with:
- ✅ Multi-account support
- ✅ Automatic rotation
- ✅ Cookie refresh
- ✅ Basic retry mechanism

However, it lacks critical features for production-grade rate limit handling:
- ❌ No cooldown periods
- ❌ No account health tracking  
- ❌ No intelligent selection
- ❌ No observability

### Recommended Next Steps

1. **Immediate** (this week): Implement Phase 1 fixes (cooldown + Retry-After parsing)
2. **Short-term** (next sprint): Add health tracking and metrics
3. **Long-term** (next quarter): Implement circuit breaker and advanced features

### Risk Assessment

**Without improvements**:
- Risk of cascading failures when multiple accounts hit rate limits
- Inefficient API usage (retrying rate-limited accounts)
- Poor observability (hard to diagnose issues)
- Potential service degradation under high load

**With improvements**:
- Graceful degradation under rate limits
- Optimized account utilization
- Better monitoring and alerting
- Improved reliability and user experience

---

## Appendix: Additional Resources

### Related Files to Review

- `middleware/`: Authentication and CORS handling
- `model/openai.go`: Response formatting
- `utils/`: Helper functions for role prefixes
- `logger/`: Logging configuration

### Perplexity API Documentation

- Rate limit information: *Not publicly documented (reverse-engineered)*
- Session token format: JWT-like token in `__Secure-next-auth.session-token` cookie
- API endpoint: `https://www.perplexity.ai/rest/sse/perplexity_ask`

### Go Libraries for Rate Limiting

Consider using:
- `golang.org/x/time/rate`: Token bucket rate limiter
- `github.com/sony/gobreaker`: Circuit breaker implementation
- `github.com/prometheus/client_golang`: Metrics export

---

**End of Analysis**
