package main

import (
	"sync"
	"time"
)

const (
	novaSonicSampleRate    = 16000
	novaSonicChannels      = 1
	novaSonicMixIntervalMs = 20
	novaSonicMixInterval   = novaSonicMixIntervalMs * time.Millisecond
	novaSonicFrameSize     = novaSonicSampleRate * novaSonicChannels * novaSonicMixIntervalMs / 1000 // 320
	novaSonicSourceLimit   = novaSonicFrameSize * 50
)

type novaSonicMixer struct {
	mu        sync.Mutex
	sources   map[string]*novaSonicSource
	output    chan []int16
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type novaSonicSource struct {
	buffer []int16
}

func newNovaSonicMixer() *novaSonicMixer {
	m := &novaSonicMixer{
		sources: make(map[string]*novaSonicSource),
		output:  make(chan []int16, 64),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go m.run()
	return m
}

func (m *novaSonicMixer) submit(trackKey string, pcm []int16) {
	if len(pcm) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	src := m.sources[trackKey]
	if src == nil {
		src = &novaSonicSource{}
		m.sources[trackKey] = src
	}
	src.buffer = append(src.buffer, pcm...)
	if overflow := len(src.buffer) - novaSonicSourceLimit; overflow > 0 {
		src.buffer = src.buffer[overflow:]
	}
}

func (m *novaSonicMixer) removeTrack(trackKey string) {
	m.mu.Lock()
	delete(m.sources, trackKey)
	m.mu.Unlock()
}

func (m *novaSonicMixer) readMixed() <-chan []int16 {
	return m.output
}

func (m *novaSonicMixer) close() {
	m.closeOnce.Do(func() {
		close(m.stop)
		<-m.done
	})
}

func (m *novaSonicMixer) run() {
	defer close(m.done)
	ticker := time.NewTicker(novaSonicMixInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			frame := m.mixFrame()
			if frame == nil {
				continue
			}
			select {
			case m.output <- frame:
			default:
				log.Warnf("Nova Sonic mixer: dropping mixed frame (consumer too slow)")
			}
		}
	}
}

func (m *novaSonicMixer) mixFrame() []int16 {
	m.mu.Lock()
	defer m.mu.Unlock()

	ready := make([]*novaSonicSource, 0, len(m.sources))
	for _, src := range m.sources {
		if len(src.buffer) >= novaSonicFrameSize {
			ready = append(ready, src)
		}
	}
	if len(ready) == 0 {
		return nil
	}

	mixed := make([]int16, novaSonicFrameSize)
	for i := range mixed {
		var sum int32
		for _, src := range ready {
			sum += int32(src.buffer[i])
		}
		mixed[i] = clampPCM16(sum / int32(len(ready)))
	}
	for _, src := range ready {
		src.buffer = src.buffer[novaSonicFrameSize:]
	}
	return mixed
}
