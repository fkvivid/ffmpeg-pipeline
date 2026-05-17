package events

import "sync"

// Broker fans out Server-Sent Events to browser subscribers per job.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan []byte
}

// NewBroker creates an empty event broker.
func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string][]chan []byte)}
}

// Subscribe returns a channel that receives events for jobID.
func (b *Broker) Subscribe(jobID string) chan []byte {
	ch := make(chan []byte, 32)
	b.mu.Lock()
	b.subscribers[jobID] = append(b.subscribers[jobID], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (b *Broker) Unsubscribe(jobID string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[jobID]
	for i, s := range subs {
		if s == ch {
			b.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends data to all subscribers of jobID (drops if slow).
func (b *Broker) Publish(jobID string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers[jobID] {
		select {
		case ch <- data:
		default:
		}
	}
}
