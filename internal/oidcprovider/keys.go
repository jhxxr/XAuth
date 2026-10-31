package oidcprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"

	"xauth/internal/store"
)

// signingKey implements op.SigningKey with the newest stored RSA key.
type signingKey struct {
	id  string
	key *rsa.PrivateKey
}

func (k *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k *signingKey) Key() any                                    { return k.key }
func (k *signingKey) ID() string                                  { return k.id }

// publicKey implements op.Key for the JWKS endpoint.
type publicKey struct {
	id  string
	key *rsa.PublicKey
}

func (k *publicKey) ID() string                         { return k.id }
func (k *publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k *publicKey) Use() string                        { return "sig" }
func (k *publicKey) Key() any                           { return k.key }

// RotateKey generates a new signing key. It becomes the current key because
// stored keys are read newest first.
func (s *Storage) RotateKey(ctx context.Context) (store.SigningKey, error) {
	k, err := generateKey()
	if err != nil {
		return store.SigningKey{}, err
	}
	row, err := saveKey(ctx, s.st, k)
	if err != nil {
		return store.SigningKey{}, err
	}
	return *row, nil
}

func saveKey(ctx context.Context, st *store.Store, k *signingKey) (*store.SigningKey, error) {
	der, err := x509.MarshalPKCS8PrivateKey(k.key)
	if err != nil {
		return nil, err
	}
	row := &store.SigningKey{ID: k.id, Algorithm: string(jose.RS256),
		PrivatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
	if err := st.CreateSigningKey(ctx, *row); err != nil {
		return nil, err
	}
	return row, nil
}

// loadKeys returns the stored signing keys, generating one on first start.
// The private key is stored unencrypted, so the database file must be
// readable only by the XAuth service account.
func loadKeys(ctx context.Context, st *store.Store) ([]*signingKey, error) {
	rows, err := st.SigningKeys(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		k, err := generateKey()
		if err != nil {
			return nil, err
		}
		if _, err := saveKey(ctx, st, k); err != nil {
			return nil, err
		}
		return []*signingKey{k}, nil
	}
	keys := make([]*signingKey, 0, len(rows))
	for _, row := range rows {
		block, _ := pem.Decode([]byte(row.PrivatePEM))
		if block == nil {
			return nil, fmt.Errorf("signing key %s: invalid PEM", row.ID)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("signing key %s: %w", row.ID, err)
		}
		rk, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("signing key %s: not an RSA key", row.ID)
		}
		keys = append(keys, &signingKey{id: row.ID, key: rk})
	}
	return keys, nil
}

func generateKey() (*signingKey, error) {
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	// The key ID is the RFC 7638 thumbprint of the public key.
	jwk := jose.JSONWebKey{Key: &rk.PublicKey}
	thumb, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, err
	}
	if len(thumb) == 0 {
		return nil, errors.New("empty key thumbprint")
	}
	return &signingKey{id: base64.RawURLEncoding.EncodeToString(thumb)[:16], key: rk}, nil
}
