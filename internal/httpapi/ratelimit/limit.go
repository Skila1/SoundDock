package ratelimit

import (
	"sync"
	"time"
)

type Class string

const (
	ClassAuth        Class = "auth"
	ClassSearch      Class = "search"
	ClassMetadata    Class = "metadata"
	ClassAdmin       Class = "admin"
	ClassIntegration Class = "integration"
	ClassExternal    Class = "external"
	ClassStreamSlot  Class = "stream"
)

type window struct {
	n     int
	reset time.Time
}

type Limiter struct {
	mu    sync.Mutex
	data  map[string]*window
	limit map[Class]int
	every time.Duration
}

func New() *Limiter {
	return &Limiter{
		data:  map[string]*window{},
		every: time.Minute,
		limit: map[Class]int{
			ClassAuth:        20,
			ClassSearch:      120,
			ClassMetadata:    60,
			ClassAdmin:       120,
			ClassIntegration: 300,
			ClassExternal:    40,
			ClassStreamSlot:  8,
		},
	}
}

func (l *Limiter) Allow(class Class, key string) bool {
	if class == ClassStreamSlot {
		return true // concurrent slots tracked separately; never 429 in-flight bodies
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := string(class) + "|" + key
	now := time.Now()
	w := l.data[k]
	if w == nil || now.After(w.reset) {
		l.data[k] = &window{n: 1, reset: now.Add(l.every)}
		return true
	}
	max := l.limit[class]
	if max == 0 {
		max = 120
	}
	if w.n >= max {
		return false
	}
	w.n++
	return true
}

type Slots struct {
	mu    sync.Mutex
	cur   map[string]int
	limit int
}

func NewSlots(n int) *Slots { return &Slots{cur: map[string]int{}, limit: n} }

func (s *Slots) Acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur[key] >= s.limit {
		return false
	}
	s.cur[key]++
	return true
}

func (s *Slots) Release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur[key]--
	if s.cur[key] <= 0 {
		delete(s.cur, key)
	}
}

func (s *Slots) Active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.cur {
		n += v
	}
	return n
}
