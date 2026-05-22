package schema

import "sync"

// Memory is the agent's append-only conversation log with a soft cap.
// Once the cap is exceeded the oldest non-system messages are dropped — system
// messages are anchored because they carry irreplaceable instructions.
type Memory struct {
	mu       sync.Mutex
	messages []Message
	max      int
}

func NewMemory(max int) *Memory {
	if max <= 0 {
		max = 200
	}
	return &Memory{max: max}
}

func (m *Memory) Add(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	if len(m.messages) > m.max {
		m.compact()
	}
}

func (m *Memory) AddMany(msgs ...Message) {
	for _, msg := range msgs {
		m.Add(msg)
	}
}

// Snapshot returns a copy of the messages; safe to pass to LLM clients
// without worrying about concurrent mutation.
func (m *Memory) Snapshot() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *Memory) Last() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return Message{}, false
	}
	return m.messages[len(m.messages)-1], true
}

func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

// compact drops the oldest non-system messages until len ≤ max.
// Called with mu held.
func (m *Memory) compact() {
	overflow := len(m.messages) - m.max
	if overflow <= 0 {
		return
	}
	kept := make([]Message, 0, m.max)
	dropped := 0
	for _, msg := range m.messages {
		if dropped < overflow && msg.Role != RoleSystem {
			dropped++
			continue
		}
		kept = append(kept, msg)
	}
	m.messages = kept
}
