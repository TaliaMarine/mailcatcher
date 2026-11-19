package credentials

import (
	"context"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memEntry struct {
	cred Credential
	hash []byte
}

// Memory is a Store that keeps logins in process memory. It is meant for
// local development and tests; logins vanish on restart.
type Memory struct {
	mu      sync.Mutex
	byName  map[string]*memEntry
	byOwner map[string]string
}

func NewMemory() *Memory {
	return &Memory{byName: map[string]*memEntry{}, byOwner: map[string]string{}}
}

func (m *Memory) Get(_ context.Context, subject string) (*Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, ok := m.byOwner[subject]
	if !ok {
		return nil, ErrNotFound
	}
	c := m.byName[name].cred
	return &c, nil
}

func (m *Memory) Set(_ context.Context, owner Owner, username, password string) (*Credential, error) {
	username = NormalizeUsername(username)
	if err := validate(username, password); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.byName[username]; ok && e.cred.OwnerSub != owner.Subject {
		return nil, ErrUsernameTaken
	}
	now := time.Now().UTC()
	created := now
	if old, ok := m.byOwner[owner.Subject]; ok {
		created = m.byName[old].cred.CreatedAt
		delete(m.byName, old)
	}
	e := &memEntry{
		cred: Credential{Username: username, OwnerSub: owner.Subject, Email: owner.Email, CreatedAt: created, UpdatedAt: now},
		hash: hash,
	}
	m.byName[username] = e
	m.byOwner[owner.Subject] = username
	c := e.cred
	return &c, nil
}

func (m *Memory) Delete(_ context.Context, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name, ok := m.byOwner[subject]; ok {
		delete(m.byName, name)
		delete(m.byOwner, subject)
	}
	return nil
}

func (m *Memory) Authenticate(_ context.Context, username, password string) (*Credential, error) {
	m.mu.Lock()
	e, ok := m.byName[NormalizeUsername(username)]
	m.mu.Unlock()
	if !ok {
		burnTime(password)
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword(e.hash, []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	c := e.cred
	return &c, nil
}
