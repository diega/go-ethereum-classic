# Security Audit Report - Go Ethereum Classic PoW Branch

**Date:** 2026-01-31
**Auditor:** Claude Code Security Analysis
**Scope:** New code added in branch `claude/security-audit-new-code-5n7zP`
**Lines Analyzed:** ~3,350 lines across 36 files

---

## Executive Summary

This report covers the security analysis of the new Go Ethereum Classic implementation, focusing on perpetual PoW chain support, MESS (Modified Exponential Subjective Scoring) artificial finality, and block propagation mechanisms.

---

## Critical Vulnerabilities

### 1. Memory Leak in Global State - `peerHeads` (HIGH)

**File:** `eth/protocols/eth/peer_pow.go:58-61`

```go
var (
    peerHeadsMu sync.RWMutex
    peerHeads   = make(map[string]*peerHeadInfo)
)
```

**Issue:** The global map `peerHeads` stores peer information but `DeletePeerHead()` is only called in `unregisterPeer()`. If a peer disconnects abruptly or errors occur during handshake after `SetPeerHead()`, the entry is never removed.

**Impact:** Memory leak that can lead to memory exhaustion under massive connect/disconnect cycles (potential DoS attack).

**Attack Vector:** An attacker can repeatedly connect and disconnect peers causing accumulation of orphaned entries.

**Recommendation:** Add periodic cleanup of stale entries or ensure deletion on all error paths.

---

### 2. Memory Leak in `peerFailures` (HIGH)

**File:** `eth/downloader/peer_pow.go:33-36`

```go
var (
    peerFailures   = make(map[string]*peerStats)
    peerFailuresMu sync.RWMutex
)
```

**Issue:** No cleanup function exists for `peerFailures`. Peers that fail and disconnect remain in the map indefinitely.

**Impact:** Memory leak under prolonged operation with many failed peers.

**Recommendation:** Add cleanup when peers are unregistered or implement TTL-based expiration.

---

### 3. Missing TD Validation in PoW Handshake (HIGH)

**File:** `eth/protocols/eth/handshake.go:226-256`

```go
func (p *Peer) Handshake68PoW(...) error {
    // ...
    if status.TD != nil && status.TD.Cmp(params.MainnetTerminalTotalDifficulty) >= 0 {
        return fmt.Errorf("peer TD %v exceeds ETH TTD, likely wrong network", status.TD)
    }
    SetPeerHead(p.id, status.Head, status.TD)  // ← status.TD can be nil!
    return nil
}
```

**Issue:** If `status.TD == nil`, nil is stored in `peerHeads`. This can cause panics in code that assumes non-nil TD, and the peer may be selected for sync with nil TD.

**Impact:** Potential panic or bypass of peer selection mechanism by highest TD.

**Recommendation:** Reject peers with nil TD or handle nil TD explicitly throughout the codebase.

---

## Medium Vulnerabilities

### 4. Race Condition in MESS Enable/Disable (MEDIUM)

**File:** `core/blockchain_mess.go:55-77`

```go
var messEnabledStatus atomic.Int32
```

**Issue:** MESS state is global (not per BlockChain instance). If multiple blockchain instances exist (tests, multiple nodes), they share state.

**Impact:** Unexpected behavior in environments with multiple chains.

**Recommendation:** Make `messEnabledStatus` a field of `BlockChain` struct.

---

### 5. MESS Bypass with Few Peers (MEDIUM)

**File:** `eth/sync_pow.go:40-41, 218-232`

```go
const minMESSPeers = 5

func (cs *chainSyncerPoW) checkMESSActivation() {
    if chainMESSEnabled && peerCount < minMESSPeers {
        cs.handler.chain.EnableMESS(false, "low peers")
    }
}
```

**Issue:** An attacker can isolate a node (eclipse attack) reducing its peers to <5 to disable MESS, then execute a deep reorg attack.

**Attack Vector:**
1. Eclipse attack to reduce peers < 5
2. MESS is automatically disabled
3. Execute 51% attack with deep reorg

**Recommendation:** Increase `minMESSPeers`, add eclipse attack detection, or require manual re-enable.

---

### 6. Potential DoS in Block Fetcher Queue (MEDIUM)

**File:** `eth/fetcher/block_fetcher_pow.go:38-39`

```go
hashLimit     = 256   // Maximum per peer
blockLimit    = 64    // Maximum per peer
```

**Issue:** Per-peer limits exist but no global limit. With many malicious peers announcing different blocks, memory in `announced`, `fetching`, `fetched`, `completing` maps can grow significantly.

**Recommendation:** Add global memory limits for block announcements.

---

### 7. Missing Block Number Validation in Announce (MEDIUM)

**File:** `eth/fetcher/block_fetcher_pow.go:353-358`

```go
if notification.number > 0 {
    if dist := int64(notification.number) - int64(height); dist < -maxUncleDist || dist > maxQueueDist {
        // rejected
    }
}
```

**Issue:** If `notification.number == 0`, distance is not validated. A peer can announce blocks with number 0 to bypass distance validation.

**Recommendation:** Make block number > 0 a required condition.

---

### 8. Sensitive Information in Logs (MEDIUM)

**File:** `eth/downloader/peer_pow.go:118-124`

```go
log.Debug("IdlePeer selection",
    "selectedPeer", selected.id[:8],
    "selectedCapacity", selectedCap,
    "maxCapacity", maxCap)
```

**Issue:** Logging exposes infrastructure information that could help attackers map the network.

**Recommendation:** Reduce verbosity of peer selection logs or make them trace-level only.

---

## Low Vulnerabilities

### 9. Use of `rand.Intn()` without Secure Seed (LOW)

**File:** `eth/fetcher/block_fetcher_pow.go:397`

```go
announce := announces[rand.Intn(len(announces))]
```

**Issue:** `math/rand` without explicit seed can be predictable.

**Recommendation:** Use `crypto/rand` or seed with secure source.

---

### 10. Potential Integer Overflow in MESS (LOW)

**File:** `core/blockchain_mess.go:121`

```go
xBig := big.NewInt(int64(current.Time - commonAncestor.Time))
```

**Issue:** If `current.Time < commonAncestor.Time` (timestamp manipulation), subtraction can underflow.

**Recommendation:** Add explicit check for timestamp ordering.

---

### 11. Potential Goroutine Leak (LOW)

**File:** `eth/fetcher/block_fetcher_pow.go:676-702`

```go
func (f *BlockFetcherPoW) insert(peer string, block *types.Block) {
    go func() {
        defer func() { f.done <- hash }()
        // ...
    }()
}
```

**Issue:** If `f.done` channel is blocked (fetcher closed), goroutine can block indefinitely.

**Recommendation:** Use select with `f.quit` channel for cancellation.

---

### 12. Missing Rate Limiting in Handshake (LOW)

**File:** `eth/protocols/eth/handshake.go:35`

```go
const handshakeTimeout = 5 * time.Second
```

**Issue:** No rate limiting for failed handshake attempts.

**Recommendation:** Add per-IP rate limiting for handshake attempts.

---

### 13. Incorrect Genesis Hash (LOW)

**File:** `params/config_etc.go:28-30`

```go
MordorGenesisHash = common.HexToHash("0xa68ebde7932f0e8c1f5e49ab8f9da6b0e0c0c68c7a4d5c5e7e0b6c3c4c3c2c1c0")
```

**Issue:** `MordorGenesisHash` appears to be a placeholder with repeating pattern, not the actual Mordor testnet genesis hash.

**Recommendation:** Verify and use the correct Mordor genesis hash.

---

## Design Observations

### 14. Shared Global State

Multiple global variables (`peerHeads`, `peerFailures`, `messEnabledStatus`) create coupling between components and complicate testing.

### 15. Missing Security Metrics

No metrics for:
- Reorg attempts rejected by MESS
- Peers disconnected for malicious behavior
- Block announcement rates per peer

### 16. Inconsistent Logging

Some critical errors are logged as `Debug` instead of `Warn` or `Error`.

---

## Severity Summary

| Severity | Count | Affected Areas |
|----------|-------|----------------|
| Critical | 3 | Peer management, Handshake, Memory management |
| Medium | 5 | MESS, Block fetcher, Logging |
| Low | 5 | Various |
| Design | 3 | General architecture |

---

## Positive Security Aspects

1. **Block integrity validation** in `handlers_pow.go:48-55` - verifies uncle hash and tx hash before processing
2. **Per-peer limits** to prevent basic DoS
3. **Correct use of `sync.RWMutex`** for shared structures
4. **Maximum TD verification** to reject ETH mainnet peers
5. **Unit tests** for MESS polynomial function

---

## Priority Recommendations

1. **Add periodic cleanup** of `peerHeads` and `peerFailures`
2. **Validate `status.TD != nil`** before storing in PoW handshake
3. **Make `messEnabledStatus` per-instance** of blockchain, not global
4. **Increase `minMESSPeers`** or implement eclipse attack detection
5. **Add global memory limit** for block announcements
6. **Validate `notification.number > 0`** mandatorily
7. **Fix Mordor genesis hash** to correct value

---

## Files Analyzed

- eth/fetcher/block_fetcher_pow.go (775 lines)
- eth/downloader/queue_pow.go (308 lines)
- eth/downloader/downloader_pow.go (243 lines)
- eth/sync_pow.go (233 lines)
- eth/protocols/eth/peer_pow.go (211 lines)
- core/blockchain_mess.go (204 lines)
- eth/handler_pow.go (157 lines)
- eth/protocols/eth/handlers_pow.go (63 lines)
- eth/handler.go (modifications)
- eth/peerset_pow.go (63 lines)
- eth/downloader/fetchers_concurrent_headers_pow.go (142 lines)
- eth/downloader/peer_pow.go (137 lines)
- eth/protocols/eth/handshake.go (modifications)
- eth/protocols/eth/protocol.go (modifications)
- params/config_etc.go (144 lines)
- core/blockchain_mess_test.go (301 lines)
- And 20 additional modified files

---

*Report generated by Claude Code Security Analysis*
