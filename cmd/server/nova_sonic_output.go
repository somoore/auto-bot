package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	novaSonicOutputPreRollFrames      = 10
	novaSonicOutputMaxPreRoll         = 240 * time.Millisecond
	novaSonicOutputMaxQueue           = 1500
	novaSonicOutputUnderrunFillFrames = 6
)

type novaSonicOutputFrameWriter func([]int16) error

type novaSonicOutputStats struct {
	QueueDepthFrames    int     `json:"queue_depth_frames"`
	QueueDepthMs        int     `json:"queue_depth_ms"`
	PreRollFrames       int     `json:"pre_roll_frames"`
	MaxQueueFrames      int     `json:"max_queue_frames"`
	DroppedFrames       uint64  `json:"dropped_frames"`
	UnderrunFrames      uint64  `json:"underrun_frames"`
	PublishedFrames     uint64  `json:"published_frames"`
	PublishPPS          float64 `json:"publish_pps"`
	LastPublishJitterMs float64 `json:"last_publish_jitter_ms"`
	MaxPublishJitterMs  float64 `json:"max_publish_jitter_ms"`
	LastPublishedAt     string  `json:"last_published_at,omitempty"`
}

type novaSonicOutputPacer struct {
	mu                    sync.Mutex
	writer                novaSonicOutputFrameWriter
	frames                [][]int16
	playing               bool
	firstFrame            time.Time
	underrunFillRemaining int

	stats      novaSonicOutputStats
	lastWrite  time.Time
	windowAt   time.Time
	windowSent uint64

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newNovaSonicOutputPacer(writer novaSonicOutputFrameWriter) *novaSonicOutputPacer {
	pacer := &novaSonicOutputPacer{
		writer: writer,
		stats: novaSonicOutputStats{
			PreRollFrames:  novaSonicOutputPreRollFrames,
			MaxQueueFrames: novaSonicOutputMaxQueue,
		},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go pacer.run()
	return pacer
}

func (pacer *novaSonicOutputPacer) EnqueueMono16(mono16k []int16) {
	if pacer == nil || len(mono16k) == 0 {
		return
	}
	frames := novaSonicOutputFramesFromMono16(mono16k)
	if len(frames) == 0 {
		return
	}

	pacer.mu.Lock()
	defer pacer.mu.Unlock()

	if len(pacer.frames) == 0 {
		pacer.firstFrame = time.Now()
	}
	pacer.frames = append(pacer.frames, frames...)
	if overflow := len(pacer.frames) - novaSonicOutputMaxQueue; overflow > 0 {
		pacer.frames = pacer.frames[overflow:]
		pacer.stats.DroppedFrames += uint64(overflow)
		pacer.firstFrame = time.Now()
	}
	pacer.updateQueueStatsLocked()
}

func (pacer *novaSonicOutputPacer) Stats() novaSonicOutputStats {
	if pacer == nil {
		return novaSonicOutputStats{}
	}
	pacer.mu.Lock()
	defer pacer.mu.Unlock()
	pacer.updateQueueStatsLocked()
	if !pacer.windowAt.IsZero() && pacer.windowSent > 0 {
		if elapsed := time.Since(pacer.windowAt); elapsed > 0 {
			pacer.stats.PublishPPS = float64(pacer.windowSent) / elapsed.Seconds()
		}
	}
	return pacer.stats
}

func (pacer *novaSonicOutputPacer) Reset() {
	if pacer == nil {
		return
	}
	pacer.mu.Lock()
	pacer.frames = nil
	pacer.playing = false
	pacer.firstFrame = time.Time{}
	pacer.underrunFillRemaining = 0
	pacer.lastWrite = time.Time{}
	pacer.windowAt = time.Time{}
	pacer.windowSent = 0
	pacer.stats = novaSonicOutputStats{
		PreRollFrames:  novaSonicOutputPreRollFrames,
		MaxQueueFrames: novaSonicOutputMaxQueue,
	}
	pacer.mu.Unlock()
}

func (pacer *novaSonicOutputPacer) Close() {
	if pacer == nil {
		return
	}
	pacer.closeOnce.Do(func() {
		close(pacer.stop)
		<-pacer.done
	})
}

func (pacer *novaSonicOutputPacer) run() {
	defer close(pacer.done)
	ticker := time.NewTicker(roomAudioMixInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pacer.stop:
			return
		case now := <-ticker.C:
			frame := pacer.nextFrame(now)
			if len(frame) == 0 {
				continue
			}
			if err := pacer.writeFrame(frame); err != nil {
				log.Errorf("Nova Sonic: paced output write failed: %v", err)
				pacer.mu.Lock()
				pacer.stats.DroppedFrames++
				pacer.mu.Unlock()
				continue
			}
			pacer.recordPublish(time.Now())
		}
	}
}

func (pacer *novaSonicOutputPacer) nextFrame(now time.Time) []int16 {
	pacer.mu.Lock()
	defer pacer.mu.Unlock()

	if len(pacer.frames) == 0 {
		if pacer.playing {
			pacer.stats.UnderrunFrames++
			if pacer.underrunFillRemaining > 0 {
				pacer.underrunFillRemaining--
				pacer.updateQueueStatsLocked()
				return make([]int16, roomAudioMixFrameSize)
			}
		}
		pacer.playing = false
		pacer.firstFrame = time.Time{}
		pacer.underrunFillRemaining = 0
		pacer.updateQueueStatsLocked()
		return nil
	}

	if !pacer.playing {
		preRollReady := len(pacer.frames) >= novaSonicOutputPreRollFrames
		preRollTimedOut := !pacer.firstFrame.IsZero() && now.Sub(pacer.firstFrame) >= novaSonicOutputMaxPreRoll
		if !preRollReady && !preRollTimedOut {
			pacer.updateQueueStatsLocked()
			return nil
		}
		pacer.playing = true
		pacer.underrunFillRemaining = novaSonicOutputUnderrunFillFrames
	}

	frame := pacer.frames[0]
	copy(pacer.frames, pacer.frames[1:])
	pacer.frames[len(pacer.frames)-1] = nil
	pacer.frames = pacer.frames[:len(pacer.frames)-1]
	if len(pacer.frames) == 0 {
		pacer.firstFrame = time.Time{}
	}
	pacer.underrunFillRemaining = novaSonicOutputUnderrunFillFrames
	pacer.updateQueueStatsLocked()
	return frame
}

func (pacer *novaSonicOutputPacer) writeFrame(frame []int16) error {
	if pacer.writer == nil {
		return fmt.Errorf("output writer is not configured")
	}
	return pacer.writer(frame)
}

func (pacer *novaSonicOutputPacer) recordPublish(now time.Time) {
	pacer.mu.Lock()
	defer pacer.mu.Unlock()

	if !pacer.lastWrite.IsZero() {
		jitter := now.Sub(pacer.lastWrite) - roomAudioMixInterval
		if jitter < 0 {
			jitter = -jitter
		}
		jitterMS := float64(jitter) / float64(time.Millisecond)
		pacer.stats.LastPublishJitterMs = jitterMS
		if jitterMS > pacer.stats.MaxPublishJitterMs {
			pacer.stats.MaxPublishJitterMs = jitterMS
		}
	}
	pacer.lastWrite = now

	if pacer.windowAt.IsZero() {
		pacer.windowAt = now
	}
	pacer.windowSent++
	if elapsed := now.Sub(pacer.windowAt); elapsed >= time.Second {
		pacer.stats.PublishPPS = float64(pacer.windowSent) / elapsed.Seconds()
		pacer.windowAt = now
		pacer.windowSent = 0
	}

	pacer.stats.PublishedFrames++
	pacer.stats.LastPublishedAt = now.UTC().Format(time.RFC3339Nano)
	pacer.updateQueueStatsLocked()
}

func (pacer *novaSonicOutputPacer) updateQueueStatsLocked() {
	pacer.stats.QueueDepthFrames = len(pacer.frames)
	pacer.stats.QueueDepthMs = int(time.Duration(len(pacer.frames)) * roomAudioMixInterval / time.Millisecond)
}

func (app *novaSonicApp) writeOutputAudioFrame(stereo48 []int16) error {
	app.mu.Lock()
	outputTrack := app.outputTrack
	enc := app.opusEnc
	app.mu.Unlock()
	if outputTrack == nil || enc == nil {
		return fmt.Errorf("livekit output track is not ready")
	}
	if len(stereo48) != roomAudioMixFrameSize {
		return fmt.Errorf("unexpected output frame samples=%d want=%d", len(stereo48), roomAudioMixFrameSize)
	}

	opusData, err := enc.Encode(stereo48)
	if err != nil {
		return fmt.Errorf("opus encode: %w", err)
	}
	if err := outputTrack.WriteSample(media.Sample{
		Data:     opusData,
		Duration: roomAudioMixInterval,
	}); err != nil {
		return fmt.Errorf("write audio sample: %w", err)
	}
	return nil
}

func novaSonicOutputFramesFromMono16(mono16k []int16) [][]int16 {
	stereo48 := upsample16kMonoTo48kStereo(mono16k)
	if len(stereo48) == 0 {
		return nil
	}

	frames := make([][]int16, 0, (len(stereo48)+roomAudioMixFrameSize-1)/roomAudioMixFrameSize)
	for offset := 0; offset < len(stereo48); offset += roomAudioMixFrameSize {
		frame := make([]int16, roomAudioMixFrameSize)
		copy(frame, stereo48[offset:min(offset+roomAudioMixFrameSize, len(stereo48))])
		frames = append(frames, frame)
	}
	return frames
}
