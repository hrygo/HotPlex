//go:build !race

package proc

// testRaceEnabled is false when building without the race detector.
// See testrace_race_test.go for the race-enabled variant.
const testRaceEnabled = false
