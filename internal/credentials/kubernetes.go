package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	labelComponent = "app.kubernetes.io/component"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelOwner     = "mailcatcher.app/owner"
	componentValue = "imap-credential"
	managedByValue = "mailcatcher"

	keyUsername = "username"
	keyHash     = "password-hash"
	keyOwnerSub = "owner-sub"
	keyEmail    = "owner-email"
	annUpdated  = "mailcatcher.app/updated-at"
)

// Kubernetes is a Store backed by one Secret per login in a single namespace.
//
// Secrets are named after a hash of the username so a login is a direct GET,
// and that same name makes the username unique (Create fails with
// AlreadyExists). A label holding a hash of the owner's subject finds the
// owner's current login.
type Kubernetes struct {
	client    kubernetes.Interface
	namespace string
	prefix    string
}

func NewKubernetes(client kubernetes.Interface, namespace, prefix string) *Kubernetes {
	return &Kubernetes{client: client, namespace: namespace, prefix: prefix}
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:32]
}

func (k *Kubernetes) secretName(username string) string {
	return fmt.Sprintf("%s-%s", k.prefix, shortHash(username)[:24])
}

func fromSecret(s *corev1.Secret) *Credential {
	c := &Credential{
		Username:  string(s.Data[keyUsername]),
		OwnerSub:  string(s.Data[keyOwnerSub]),
		Email:     string(s.Data[keyEmail]),
		CreatedAt: s.CreationTimestamp.Time,
		UpdatedAt: s.CreationTimestamp.Time,
	}
	if t, err := time.Parse(time.RFC3339, s.Annotations[annUpdated]); err == nil {
		c.UpdatedAt = t
	}
	return c
}

func (k *Kubernetes) findByOwner(ctx context.Context, subject string) (*corev1.Secret, error) {
	list, err := k.client.CoreV1().Secrets(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s,%s=%s",
			labelManagedBy, managedByValue, labelComponent, componentValue, labelOwner, shortHash(subject)),
	})
	if err != nil {
		return nil, fmt.Errorf("list credential secrets: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, ErrNotFound
	}
	return &list.Items[0], nil
}

func (k *Kubernetes) Get(ctx context.Context, subject string) (*Credential, error) {
	s, err := k.findByOwner(ctx, subject)
	if err != nil {
		return nil, err
	}
	return fromSecret(s), nil
}

func (k *Kubernetes) build(owner Owner, username string, hash []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.secretName(username),
			Namespace: k.namespace,
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelComponent: componentValue,
				labelOwner:     shortHash(owner.Subject),
			},
			Annotations: map[string]string{annUpdated: time.Now().UTC().Format(time.RFC3339)},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			keyUsername: []byte(username),
			keyHash:     hash,
			keyOwnerSub: []byte(owner.Subject),
			keyEmail:    []byte(owner.Email),
		},
	}
}

func (k *Kubernetes) Set(ctx context.Context, owner Owner, username, password string) (*Credential, error) {
	username = NormalizeUsername(username)
	if err := validate(username, password); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	secrets := k.client.CoreV1().Secrets(k.namespace)
	existing, err := k.findByOwner(ctx, owner.Subject)
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	desired := k.build(owner, username, hash)

	// Same username as the owner's current login: update in place.
	if existing != nil && existing.Name == desired.Name {
		desired.ResourceVersion = existing.ResourceVersion
		desired.CreationTimestamp = existing.CreationTimestamp
		out, err := secrets.Update(ctx, desired, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("update credential secret: %w", err)
		}
		return fromSecret(out), nil
	}

	out, err := secrets.Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create credential secret: %w", err)
	}
	if existing != nil { // the username changed: drop the old login
		if err := secrets.Delete(ctx, existing.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("delete previous credential secret: %w", err)
		}
	}
	return fromSecret(out), nil
}

func (k *Kubernetes) Delete(ctx context.Context, subject string) error {
	s, err := k.findByOwner(ctx, subject)
	if err == ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := k.client.CoreV1().Secrets(k.namespace).Delete(ctx, s.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete credential secret: %w", err)
	}
	return nil
}

func (k *Kubernetes) Authenticate(ctx context.Context, username, password string) (*Credential, error) {
	username = NormalizeUsername(username)
	if !usernameRe.MatchString(username) { // cannot exist; skip the API call
		burnTime(password)
		return nil, ErrInvalidCredentials
	}
	s, err := k.client.CoreV1().Secrets(k.namespace).Get(ctx, k.secretName(username), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		burnTime(password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("get credential secret: %w", err)
	}
	// The name is derived from the username, but verify rather than trust it,
	// and ignore Secrets that merely share the prefix.
	if s.Labels[labelComponent] != componentValue || string(s.Data[keyUsername]) != username {
		burnTime(password)
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword(s.Data[keyHash], []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return fromSecret(s), nil
}
