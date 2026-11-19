// Package credentials stores the IMAP logins users create for themselves.
//
// Each login is one Kubernetes Secret holding the username and a bcrypt hash
// of the password; the plaintext password is never stored. A user (identified
// by their OIDC subject) owns at most one login.
package credentials

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound           = errors.New("credential not found")
	ErrUsernameTaken      = errors.New("username is already in use")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

// ValidationError describes input the user can fix.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

const (
	minPasswordLen = 12
	// bcrypt ignores everything after 72 bytes; refuse rather than truncate.
	maxPasswordLen = 72
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{2,63}$`)

// Owner identifies the OIDC user a credential belongs to.
type Owner struct {
	Subject string
	Email   string
}

// Credential is the non-secret view of a stored login.
type Credential struct {
	Username  string    `json:"username"`
	OwnerSub  string    `json:"-"`
	Email     string    `json:"owner_email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store persists IMAP logins.
type Store interface {
	// Get returns the login owned by the subject, or ErrNotFound.
	Get(ctx context.Context, subject string) (*Credential, error)
	// Set creates or replaces the owner's login. Changing the username
	// removes the previous one. Returns ErrUsernameTaken if another user has it.
	Set(ctx context.Context, owner Owner, username, password string) (*Credential, error)
	// Delete removes the owner's login. Deleting a missing login is not an error.
	Delete(ctx context.Context, subject string) error
	// Authenticate checks an IMAP login, returning ErrInvalidCredentials on
	// an unknown user or a wrong password.
	Authenticate(ctx context.Context, username, password string) (*Credential, error)
}

// NormalizeUsername lowercases and trims a username.
func NormalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

func validate(username, password string) error {
	if !usernameRe.MatchString(username) {
		return &ValidationError{"username must be 3-64 characters: lowercase letters, digits and . _ @ + -, starting with a letter or digit"}
	}
	if len(password) < minPasswordLen {
		return &ValidationError{fmt.Sprintf("password must be at least %d characters", minPasswordLen)}
	}
	if len(password) > maxPasswordLen {
		return &ValidationError{fmt.Sprintf("password must be at most %d bytes", maxPasswordLen)}
	}
	return nil
}

// bcryptCost is a variable so tests can lower it.
var bcryptCost = bcrypt.DefaultCost

func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
}

// dummyHash lets Authenticate spend the same time on unknown users as on
// wrong passwords, so response time does not reveal which usernames exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("mailcatcher-dummy"), bcrypt.DefaultCost)

func burnTime(password string) { _ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password)) }
