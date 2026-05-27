package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
)

// ToOpus converts audio bytes (MP3 or WAV) to Ogg/Opus format (24kHz mono)
// suitable for Feishu audio messages. Requires ffmpeg at runtime.
// ffmpeg auto-detects input format from stream header.
func ToOpus(ctx context.Context, audioData []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-ar", "24000",
		"-ac", "1",
		"-acodec", "libopus",
		"-f", "ogg",
		"-hide_banner",
		"-loglevel", "error",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audioData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg audio→opus: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg audio→opus: empty output")
	}
	return out, nil
}

// ToMP3 converts audio bytes (WAV or any format) to MP3 format (24kHz mono)
// suitable for Slack audio messages. Requires ffmpeg at runtime.
// If input is already MP3 (detected by ID3 or MPEG sync header), returns unchanged.
func ToMP3(ctx context.Context, audioData []byte) ([]byte, error) {
	if isMP3(audioData) {
		return audioData, nil
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-ar", "24000",
		"-ac", "1",
		"-acodec", "libmp3lame",
		"-b:a", "48k",
		"-f", "mp3",
		"-hide_banner",
		"-loglevel", "error",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audioData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg audio→mp3: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg audio→mp3: empty output")
	}
	return out, nil
}

// isMP3 detects MP3 audio by checking for ID3v2 header or MPEG audio sync word.
func isMP3(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	// ID3v2 header: "ID3"
	if data[0] == 0x49 && data[1] == 0x44 && data[2] == 0x33 {
		return true
	}
	// MPEG audio sync word: 0xFF followed by 0xE0 mask (bits 7-5 set).
	if data[0] == 0xFF && len(data) >= 2 && (data[1]&0xE0) == 0xE0 && data[1] != 0xFF {
		return true
	}
	return false
}

// EstimateAudioDuration estimates audio duration in seconds from audio bytes.
// Assumes 48kbps mono ≈ 6000 bytes/sec. Used for logging MP3 output.
func EstimateAudioDuration(audioBytes int) int {
	if audioBytes <= 0 {
		return 1
	}
	secs := audioBytes / 6000
	if secs < 1 {
		return 1
	}
	return secs
}

// EstimateAudioDurationMs returns audio duration in milliseconds.
// Used for Slack TTS logging where exact duration is not required.
func EstimateAudioDurationMs(audioBytes int) int {
	return EstimateAudioDuration(audioBytes) * 1000
}

// ParseOggDurationMs extracts duration in milliseconds from Ogg container metadata.
// It scans Ogg pages for the highest granule position to compute duration.
// Per RFC 7845, Ogg Opus granule position is always at 48 kHz regardless of output sample rate.
// Returns 0 if the data is not a valid Ogg stream.
func ParseOggDurationMs(data []byte) int {
	const opusInternalRate = 48000
	if len(data) < 27 {
		return 0
	}
	var lastGranule uint64
	for i := 0; i <= len(data)-27; {
		if data[i] == 'O' && data[i+1] == 'g' && data[i+2] == 'g' && data[i+3] == 'S' {
			granule := binary.LittleEndian.Uint64(data[i+6 : i+14])
			if granule > lastGranule {
				lastGranule = granule
			}
			numSegments := int(data[i+26])
			if i+27+numSegments > len(data) {
				break
			}
			bodySize := 0
			for j := 0; j < numSegments; j++ {
				bodySize += int(data[i+27+j])
			}
			pageSize := 27 + numSegments + bodySize
			if pageSize <= 0 || i+pageSize > len(data) {
				break
			}
			i += pageSize
		} else {
			i++
		}
	}
	if lastGranule == 0 {
		return 0
	}
	return int(lastGranule / (opusInternalRate / 1000))
}
