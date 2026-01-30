package memory

import (
	"context"
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

type MessageRecord struct {
	Role    string
	Content string
	Time    time.Time
}

type VectorRecord struct {
	Text      string
	Embedding []float64
	Time      time.Time
}

type ChannelMemory struct {
	Summary  string
	Messages []MessageRecord
	Vectors  []VectorRecord
}

type EmbedFn func(ctx context.Context, input string) ([]float64, error)

type Store struct {
	mu      sync.Mutex
	byChID  map[string]*ChannelMemory
	embedFn EmbedFn
}

func NewStore(embedFn EmbedFn) *Store {
	return &Store{
		byChID:  make(map[string]*ChannelMemory),
		embedFn: embedFn,
	}
}

func (s *Store) AddMessage(ctx context.Context, chID, role, content string) {
	mem := s.getOrCreate(chID)
	s.mu.Lock()
	mem.Messages = append(mem.Messages, MessageRecord{
		Role:    role,
		Content: content,
		Time:    time.Now(),
	})
	s.mu.Unlock()
	if role == "user" {
		go s.indexMessage(ctx, chID, content)
	}
}

func (s *Store) SummaryAndRecent(chID string) (summary string, messages []MessageRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem := s.byChID[chID]
	if mem == nil {
		return "", nil
	}
	return mem.Summary, append([]MessageRecord(nil), mem.Messages...)
}

func (s *Store) SetSummary(chID, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem := s.byChID[chID]
	if mem == nil {
		mem = &ChannelMemory{}
		s.byChID[chID] = mem
	}
	mem.Summary = summary
}

func (s *Store) TrimHistory(chID string, keep int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem := s.byChID[chID]
	if mem == nil {
		return
	}
	if len(mem.Messages) > keep {
		mem.Messages = mem.Messages[len(mem.Messages)-keep:]
	}
}

func (s *Store) TopK(chID string, query []float64, k int) []string {
	s.mu.Lock()
	mem := s.byChID[chID]
	if mem == nil {
		s.mu.Unlock()
		return nil
	}
	vectors := append([]VectorRecord(nil), mem.Vectors...)
	s.mu.Unlock()
	type scored struct {
		text  string
		score float64
	}
	scoredList := make([]scored, 0, len(vectors))
	for _, vec := range vectors {
		score := cosineSimilarity(query, vec.Embedding)
		scoredList = append(scoredList, scored{text: vec.Text, score: score})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})
	if len(scoredList) > k {
		scoredList = scoredList[:k]
	}
	results := make([]string, 0, len(scoredList))
	for _, item := range scoredList {
		results = append(results, item.text)
	}
	return results
}

func (s *Store) getOrCreate(chID string) *ChannelMemory {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem := s.byChID[chID]
	if mem == nil {
		mem = &ChannelMemory{}
		s.byChID[chID] = mem
	}
	return mem
}

func (s *Store) indexMessage(ctx context.Context, chID, content string) {
	embedding, err := s.embedFn(ctx, content)
	if err != nil {
		log.Printf("embedding error: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mem := s.byChID[chID]
	if mem == nil {
		mem = &ChannelMemory{}
		s.byChID[chID] = mem
	}
	mem.Vectors = append(mem.Vectors, VectorRecord{
		Text:      content,
		Embedding: embedding,
		Time:      time.Now(),
	})
	if len(mem.Vectors) > 200 {
		mem.Vectors = mem.Vectors[len(mem.Vectors)-200:]
	}
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
