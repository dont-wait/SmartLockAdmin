package otp

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

type Record struct {
	DeviceID  string
	Email     string
	Code      string
	ExpiresAt int64
}
type Service struct {
	mu      sync.Mutex
	records map[string]Record
}

func New() *Service { return &Service{records: make(map[string]Record)} }

func (s *Service) Issue(deviceID, email string) (Record, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return Record{}, err
	}
	record := Record{DeviceID: deviceID, Email: email, Code: fmt.Sprintf("%06d", n.Int64()), ExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli()}
	s.mu.Lock()
	s.records[deviceID] = record
	s.mu.Unlock()
	return record, nil
}

func (s *Service) Verify(deviceID, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[deviceID]
	if !ok {
		return false
	}
	if record.ExpiresAt <= time.Now().UnixMilli() {
		delete(s.records, deviceID)
		return false
	}
	if record.Code != code {
		return false
	}
	delete(s.records, deviceID)
	return true
}
