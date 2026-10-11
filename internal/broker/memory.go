package broker

import (
	"errors"
	"log/slog"
	"sync"
)

var ErrClosed = errors.New("broker closed")

// Memory is an in-process broker. Each subscription gets its own buffered
// queue and goroutine, so a slow consumer never blocks the publisher or other
// subscribers (QoS 0 semantics: messages are dropped when the queue is full).
type Memory struct {
	mu     sync.RWMutex
	subs   map[int]*subscription
	nextID int
	closed bool
	log    *slog.Logger
}

type subscription struct {
	filter string
	ch     chan Message
	done   chan struct{}
}

const queueSize = 1024

func NewMemory(log *slog.Logger) *Memory {
	return &Memory{subs: map[int]*subscription{}, log: log}
}

func (b *Memory) Publish(topic string, payload []byte) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrClosed
	}
	msg := Message{Topic: topic, Payload: payload}
	for _, s := range b.subs {
		if !Match(s.filter, topic) {
			continue
		}
		select {
		case s.ch <- msg:
		default:
			b.log.Warn("broker queue full, dropping message", "filter", s.filter, "topic", topic)
		}
	}
	return nil
}

func (b *Memory) Subscribe(filter string, h Handler) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	s := &subscription{filter: filter, ch: make(chan Message, queueSize), done: make(chan struct{})}
	id := b.nextID
	b.nextID++
	b.subs[id] = s

	go func() {
		defer close(s.done)
		for msg := range s.ch {
			h(msg)
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(s.ch)
			}
			b.mu.Unlock()
			<-s.done
		})
	}, nil
}

func (b *Memory) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, s := range b.subs {
		close(s.ch)
		delete(b.subs, id)
	}
}
