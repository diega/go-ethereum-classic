package metrics

import "time"

// forcedArbiter ticks meters that must produce rates even while the metrics
// system is disabled. It mirrors arbiter, minus the metricsEnabled gate in
// its loop.
var forcedArbiter = meterTicker{meters: make(map[*Meter]struct{})}

// NewMeterForced constructs a new Meter and launches a goroutine no matter
// the global switch is enabled or not. Forced meters cannot be stopped:
// they tick for the lifetime of the process.
func NewMeterForced() *Meter {
	m := newMeter()
	forcedArbiter.add(m)
	forcedArbiter.once.Do(func() { go forcedArbiter.loopForced() })
	return m
}

// loopForced ticks meters on a 5-second interval regardless of whether the
// metrics system is enabled.
func (ma *meterTicker) loopForced() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		ma.mu.RLock()
		for meter := range ma.meters {
			meter.tick()
		}
		ma.mu.RUnlock()
	}
}
