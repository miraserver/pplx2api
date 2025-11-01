# Rate Limit Handling - Quick Reference

> **TL;DR**: Basic multi-account rotation exists, but lacks intelligent rate limit handling. Automatic retry works, but doesn't skip rate-limited accounts or implement cooldowns.

---

## ✅ What Works

- **Multi-account support**: Loads comma-separated session tokens from `SESSIONS` env var
- **Round-robin rotation**: `SessionRagen.NextIndex()` cycles through accounts
- **429 detection**: Explicitly checks for `http.StatusTooManyRequests` in `core/api.go:221`
- **Automatic retry**: Tries all available sessions on any error (`service/handle.go:116-160`)
- **Cookie refresh**: Background job updates tokens every 24 hours (`job/cookie.go`)
- **Thread safety**: Proper mutex usage for concurrent access

---

## ❌ What's Missing

### Critical Gaps

1. **No cooldown periods**
   - Rate-limited accounts are immediately available for next request
   - Can spam the same rate-limited account repeatedly

2. **No account health tracking**
   - No state stored per account (error counts, last used, health status)
   - Can't prioritize healthy accounts

3. **No Retry-After header parsing**
   - Ignores server's cooldown hints
   - Uses no delay at all between retries

4. **Treats all errors the same**
   - 429, auth errors, network errors all trigger same retry logic
   - Could be smarter about which errors to retry

### Medium Gaps

5. **No observability**
   - No metrics on per-account usage or health
   - Only basic error logging

6. **No exponential backoff**
   - Retries immediately with next account

7. **Inefficient rotation**
   - Simple round-robin can be predictable
   - No load balancing or least-recently-used

---

## 📊 Current Architecture

```
Environment Variables (SESSIONS)
    ↓
config.LoadConfig() → []SessionInfo
    ↓
SessionRagen.NextIndex() → Round-robin selection
    ↓
service.ChatCompletionsHandler() → Retry loop
    ↓
core.SendMessage() → Detect 429
    ↓
Retry with next session (no cooldown!)
```

---

## 🎯 Recommended Fixes (Priority Order)

### Phase 1: Quick Wins (4-5 hours total)

1. **Add rate limit cooldown** ⭐ **HIGH PRIORITY**
   ```go
   type SessionInfo struct {
       SessionKey      string
       IsRateLimited   bool         // NEW
       RateLimitExpiry time.Time    // NEW
   }
   ```
   - Set cooldown when 429 detected (default 60s)
   - Skip rate-limited sessions in retry loop
   - **Impact**: Prevents spamming rate-limited accounts

2. **Parse Retry-After header** ⭐ **HIGH PRIORITY**
   - Extract cooldown duration from response
   - Use server's value instead of hardcoded 60s
   - **Impact**: Respects API's rate limit hints

3. **Fix index increment bug**
   - Line `service/handle.go:121` has redundant increment
   - **Impact**: Cleaner code, predictable rotation

### Phase 2: Health Tracking (8-10 hours)

4. **Expand SessionInfo with health metrics**
   - Add: `LastUsed`, `ErrorCount`, `SuccessCount`, `LastError`
   - Track account reliability

5. **Implement health-based selection**
   - Replace round-robin with "least recently used + healthy"
   - Skip unhealthy accounts automatically

6. **Add basic metrics/logging**
   - Per-account request counts
   - Rate limit event tracking

### Phase 3: Advanced (15-20 hours)

7. Circuit breaker pattern
8. Exponential backoff
9. Request queuing
10. Prometheus metrics

---

## 🔍 Code Locations

| Component | File | Lines | Description |
|-----------|------|-------|-------------|
| Config loading | `config/config.go` | 43-62 | Parse SESSIONS env var |
| Session rotation | `config/config.go` | 127-134 | Round-robin NextIndex() |
| Retry loop | `service/handle.go` | 116-160 | Try all sessions on error |
| 429 detection | `core/api.go` | 221-224 | HTTP status check |
| Cookie refresh | `job/cookie.go` | 162-211 | Background update job |
| Job start | `main.go` | 19-22 | 24-hour interval |

---

## 🧪 Test Coverage Needed

- [ ] Single account hits rate limit → switches to next
- [ ] All accounts rate limited → proper error message
- [ ] Cooldown expires → account becomes available again  
- [ ] Concurrent requests → no race conditions
- [ ] Retry-After header → correctly parsed and applied
- [ ] Cookie refresh → doesn't break rate limit tracking

---

## 📈 Impact Analysis

### Without Fixes
- ⚠️ Risk: Cascading failures under load
- ⚠️ Inefficiency: Wasted retries on rate-limited accounts
- ⚠️ Poor UX: Unnecessary delays and errors
- ⚠️ No visibility: Can't monitor account health

### With Phase 1 Fixes
- ✅ 50-70% reduction in wasted retries
- ✅ Faster failover to healthy accounts
- ✅ Respects API rate limits properly
- ✅ ~4-5 hours implementation time

### With All Phases
- ✅ Production-ready rate limit handling
- ✅ Full observability and metrics
- ✅ Automatic account health management
- ✅ Optimized resource utilization

---

## 🚀 Quick Start: Implementing Cooldown

**Minimal change to start improving immediately:**

```go
// 1. In config/config.go - Add to SessionInfo:
type SessionInfo struct {
    SessionKey        string
    IsRateLimited     bool
    RateLimitExpiry   time.Time
}

// 2. In core/api.go - When 429 detected:
if resp.StatusCode == http.StatusTooManyRequests {
    session.IsRateLimited = true
    session.RateLimitExpiry = time.Now().Add(60 * time.Second)
    // ...
}

// 3. In service/handle.go - Skip rate-limited sessions:
for i := 0; i < config.ConfigInstance.RetryCount; i++ {
    session, _ := config.ConfigInstance.GetSessionForModel(index)
    
    // NEW: Check availability
    if session.IsRateLimited && time.Now().Before(session.RateLimitExpiry) {
        continue // Skip this session
    }
    
    // ... rest of retry logic
}
```

**Result**: Immediate improvement with minimal code changes!

---

## 📚 Related Documentation

- Full analysis: `RATE_LIMIT_ANALYSIS.md`
- Environment config: `.env.example`
- API documentation: `README.md`

---

**Document Version**: 1.0  
**Last Updated**: 2024-11-01  
**Status**: Analysis Complete
