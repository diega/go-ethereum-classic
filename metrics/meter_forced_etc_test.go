package metrics

import "testing"

// TestMeterForcedUsesUngatedTicker guards the revived NewMeterForced
// (#17667): the ethash hashrate meter must produce rates even when the node
// runs without --metrics. A meter registered with the regular arbiter is
// never ticked while metrics are disabled, so its one-minute rate stays 0
// forever and eth_hashrate / netstats report 0 despite active sealing.
//
// init_test.go forces metricsEnabled=true for the whole package, so the
// disabled path cannot be exercised directly; being registered with the
// arbiter whose loop has no metricsEnabled gate is the property that makes
// the meter work in that state.
func TestMeterForcedUsesUngatedTicker(t *testing.T) {
	m := NewMeterForced()

	forcedArbiter.mu.RLock()
	_, registered := forcedArbiter.meters[m]
	forcedArbiter.mu.RUnlock()
	if !registered {
		t.Fatal("forced meter not registered with the forced arbiter")
	}

	// Folding marks on tick must yield a non-zero rate.
	m.Mark(1000)
	m.tick()
	if rate := m.Snapshot().Rate1(); rate == 0 {
		t.Fatal("forced meter Rate1 = 0 after Mark+tick; hashrate would report 0")
	}
}
