//go:build race

package proc

// testRaceEnabled is true when building with -race.
// Real process spawning tests are skipped under race detector
// because ThreadSanitizer's internal allocator runs out of memory
// when tracking concurrent OS process operations (pipes, PGID, etc.).
const testRaceEnabled = true
