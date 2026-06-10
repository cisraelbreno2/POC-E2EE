package clientcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

func GenerateEd25519KeyPair() (ed25519.PrivateKey, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	return priv, base64.StdEncoding.EncodeToString(pub), nil
}

func GenerateX25519KeyPair() ([]byte, string, error) {
	var priv, pub [32]byte
	if _, err := io.ReadFull(rand.Reader, priv[:]); err != nil {
		return nil, "", err
	}
	curve25519.ScalarBaseMult(&pub, &priv)
	return priv[:], base64.StdEncoding.EncodeToString(pub[:]), nil
}

func GenerateEphemeralX25519KeyPair() ([]byte, []byte, error) {
	var priv, pub [32]byte
	if _, err := io.ReadFull(rand.Reader, priv[:]); err != nil {
		return nil, nil, err
	}
	curve25519.ScalarBaseMult(&pub, &priv)
	return priv[:], pub[:], nil
}

func EncryptMessageAES(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	output := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(output), nil
}

func DecryptMessageAES(ciphertextBase64 string, key []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func EcdheDeriveKeySender(recipientDhPubBase64 string) ([]byte, string, error) {
	recipientPubBytes, err := base64.StdEncoding.DecodeString(recipientDhPubBase64)
	if err != nil || len(recipientPubBytes) != 32 {
		return nil, "", errors.New("invalid recipient dh pub key")
	}

	ephemeralPriv, ephemeralPub, err := GenerateEphemeralX25519KeyPair()
	if err != nil {
		return nil, "", err
	}

	sharedSecret, err := curve25519.X25519(ephemeralPriv, recipientPubBytes)
	if err != nil {
		return nil, "", err
	}

	hkdfReader := hkdf.New(sha256.New, sharedSecret, nil, []byte("e2ee-poc-info"))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, "", err
	}

	return aesKey, base64.StdEncoding.EncodeToString(ephemeralPub), nil
}

func EcdheDeriveKeyReceiver(senderEphemeralPubBase64 string, receiverDhPriv []byte) ([]byte, error) {
	senderPubBytes, err := base64.StdEncoding.DecodeString(senderEphemeralPubBase64)
	if err != nil || len(senderPubBytes) != 32 {
		return nil, errors.New("invalid sender ephemeral pub key")
	}

	sharedSecret, err := curve25519.X25519(receiverDhPriv, senderPubBytes)
	if err != nil {
		return nil, err
	}

	hkdfReader := hkdf.New(sha256.New, sharedSecret, nil, []byte("e2ee-poc-info"))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, err
	}

	return aesKey, nil
}

func SignMessageEd25519(payload string, privateKey ed25519.PrivateKey) (string, error) {
	signature := ed25519.Sign(privateKey, []byte(payload))
	return base64.StdEncoding.EncodeToString(signature), nil
}

func VerifySignatureEd25519(payload string, signatureBase64 string, senderIdentityPubBase64 string) error {
	pubBytes, err := base64.StdEncoding.DecodeString(senderIdentityPubBase64)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("invalid sender identity pub key")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return err
	}

	if !ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(payload), sigBytes) {
		return errors.New("invalid signature")
	}
	return nil
}
