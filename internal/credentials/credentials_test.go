package credentials

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func stores(t *testing.T) map[string]Store {
	t.Helper()
	return map[string]Store{
		"memory":     NewMemory(),
		"kubernetes": NewKubernetes(fake.NewClientset(), "mail", "mc-imap"),
	}
}

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	alice := Owner{Subject: "sub-alice", Email: "alice@example.com"}
	bob := Owner{Subject: "sub-bob", Email: "bob@example.com"}

	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Get(ctx, alice.Subject); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get before Set: got %v, want ErrNotFound", err)
			}

			if _, err := s.Set(ctx, alice, "Alice", "correct horse battery"); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got, err := s.Get(ctx, alice.Subject)
			if err != nil || got.Username != "alice" {
				t.Fatalf("Get = %+v, %v", got, err)
			}

			if _, err := s.Authenticate(ctx, "ALICE", "correct horse battery"); err != nil {
				t.Fatalf("Authenticate (case-insensitive username): %v", err)
			}
			if _, err := s.Authenticate(ctx, "alice", "wrong password!!"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("wrong password: got %v", err)
			}
			if _, err := s.Authenticate(ctx, "nobody", "whatever password"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("unknown user: got %v", err)
			}

			// Another user cannot take the username.
			if _, err := s.Set(ctx, bob, "alice", "another long password"); !errors.Is(err, ErrUsernameTaken) {
				t.Fatalf("duplicate username: got %v", err)
			}

			// Password change keeps the username.
			if _, err := s.Set(ctx, alice, "alice", "a brand new password"); err != nil {
				t.Fatalf("password change: %v", err)
			}
			if _, err := s.Authenticate(ctx, "alice", "correct horse battery"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatal("old password still works")
			}

			// Renaming replaces the old login.
			if _, err := s.Set(ctx, alice, "alice.new", "a brand new password"); err != nil {
				t.Fatalf("rename: %v", err)
			}
			if _, err := s.Authenticate(ctx, "alice", "a brand new password"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatal("old username still works after rename")
			}
			if _, err := s.Authenticate(ctx, "alice.new", "a brand new password"); err != nil {
				t.Fatalf("new username: %v", err)
			}
			// ...and frees the old name.
			if _, err := s.Set(ctx, bob, "alice", "another long password"); err != nil {
				t.Fatalf("freed username: %v", err)
			}

			if err := s.Delete(ctx, alice.Subject); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if err := s.Delete(ctx, alice.Subject); err != nil {
				t.Fatalf("second Delete: %v", err)
			}
			if _, err := s.Authenticate(ctx, "alice.new", "a brand new password"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatal("deleted login still works")
			}
		})
	}
}

func TestValidation(t *testing.T) {
	o := Owner{Subject: "s"}
	cases := []struct{ user, pass string }{
		{"ab", "long enough password"},
		{"Has Space", "long enough password"},
		{"-leading", "long enough password"},
		{"valid.user", "short"},
		{"valid.user", string(make([]byte, 73))},
	}
	for name, s := range stores(t) {
		for _, c := range cases {
			_, err := s.Set(context.Background(), o, c.user, c.pass)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: Set(%q, %d bytes) = %v, want ValidationError", name, c.user, len(c.pass), err)
			}
		}
	}
}

func TestKubernetesDoesNotStorePlaintext(t *testing.T) {
	client := fake.NewClientset()
	s := NewKubernetes(client, "mail", "mc-imap")
	if _, err := s.Set(context.Background(), Owner{Subject: "x"}, "carol", "super secret pass"); err != nil {
		t.Fatal(err)
	}
	list, _ := client.CoreV1().Secrets("mail").List(context.Background(), metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("got %d secrets", len(list.Items))
	}
	for k, v := range list.Items[0].Data {
		if string(v) == "super secret pass" {
			t.Fatalf("plaintext password stored under %q", k)
		}
	}
}

func TestKubernetesIgnoresForeignSecrets(t *testing.T) {
	client := fake.NewClientset()
	s := NewKubernetes(client, "mail", "mc-imap")
	// A Secret that happens to have the derived name but is not ours.
	_, _ = client.CoreV1().Secrets("mail").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: s.secretName("mallory")},
		Data:       map[string][]byte{keyUsername: []byte("mallory"), keyHash: []byte("junk")},
	}, metav1.CreateOptions{})
	if _, err := s.Authenticate(context.Background(), "mallory", "whatever password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v", err)
	}
}
