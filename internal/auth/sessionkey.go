package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const sessionKeyBytes = 32

// LoadSessionKey returns the key that signs session cookies. In order of
// preference it uses the configured value, a Kubernetes Secret (created with
// a random key if missing, so a GitOps-managed chart needs no secret
// material), and finally a random in-memory key.
func LoadSessionKey(ctx context.Context, configured string, client kubernetes.Interface, namespace, secretName, secretKey string, logger *slog.Logger) ([]byte, error) {
	if configured != "" {
		if len(configured) < sessionKeyBytes {
			return nil, fmt.Errorf("SESSION_KEY must be at least %d characters", sessionKeyBytes)
		}
		return []byte(configured), nil
	}

	if client != nil && secretName != "" {
		secrets := client.CoreV1().Secrets(namespace)
		s, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			key, err := randomKey()
			if err != nil {
				return nil, err
			}
			s, err = secrets.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   secretName,
					Labels: map[string]string{"app.kubernetes.io/managed-by": "mailcatcher"},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{secretKey: key},
			}, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) { // another replica won the race
				s, err = secrets.Get(ctx, secretName, metav1.GetOptions{})
			}
			if err != nil {
				return nil, fmt.Errorf("create session key secret: %w", err)
			}
			logger.Info("created session key secret", "name", secretName)
			if v, ok := s.Data[secretKey]; ok && len(v) >= sessionKeyBytes {
				return v, nil
			}
			return []byte(key), nil
		}
		if err != nil {
			return nil, fmt.Errorf("get session key secret: %w", err)
		}
		v := s.Data[secretKey]
		if len(v) < sessionKeyBytes {
			return nil, fmt.Errorf("secret %s key %q must hold at least %d bytes", secretName, secretKey, sessionKeyBytes)
		}
		return v, nil
	}

	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	logger.Warn("no SESSION_KEY or session key secret configured, using a random key: sessions end on restart and are not shared between replicas")
	return []byte(key), nil
}

func randomKey() (string, error) {
	b := make([]byte, sessionKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
