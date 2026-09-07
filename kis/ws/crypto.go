package ws

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
)

// aesMaterial is the AES-CBC key and IV a single tr_id's subscribe ACK
// delivered. Material is per tr_id: a key issued for one stream does not
// decrypt another.
type aesMaterial struct {
	key []byte
	iv  []byte
}

var (
	errNoMaterial   = errors.New("ws: no decryption material for transaction")
	errBadMaterial  = errors.New("ws: decryption material has an unusable length")
	errBadCipher    = errors.New("ws: encrypted payload is not decryptable")
	errShortPayload = errors.New("ws: encrypted payload is truncated")
)

// keyLengths and ivLengths are the byte lengths AES-CBC accepts here.
var (
	keyLengths = []int{16, 24, 32}
	ivLengths  = []int{16}
)

// decodeMaterial turns an ACK's key or iv field into raw bytes.
//
// KIS delivers the material either literally, where the UTF-8 bytes already
// have the expected length, or base64-encoded. Literal is tried first because
// a literal 24-character key is also valid base64 and would otherwise decode
// to the wrong 18 bytes. Anything that is neither is rejected rather than
// truncated or padded into shape.
func decodeMaterial(raw string, want []int) ([]byte, error) {
	literal := []byte(raw)
	if hasLength(len(literal), want) {
		return literal, nil
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err == nil && hasLength(len(decoded), want) {
		return decoded, nil
	}
	return nil, errBadMaterial
}

func hasLength(n int, want []int) bool {
	for _, w := range want {
		if n == w {
			return true
		}
	}
	return false
}

// newMaterial validates an ACK's key/iv pair. It never logs or wraps the
// material itself into the returned error.
func newMaterial(rawKey, rawIV string) (aesMaterial, error) {
	key, err := decodeMaterial(rawKey, keyLengths)
	if err != nil {
		return aesMaterial{}, err
	}
	iv, err := decodeMaterial(rawIV, ivLengths)
	if err != nil {
		return aesMaterial{}, err
	}
	return aesMaterial{key: key, iv: iv}, nil
}

// decrypt turns a base64 AES-CBC ciphertext into its UTF-8 plaintext.
func (m aesMaterial) decrypt(payload string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return "", errBadCipher
	}
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return "", errBadMaterial
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return "", errShortPayload
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, m.iv).CryptBlocks(plain, ciphertext)
	unpadded, err := unpadPKCS7(plain, block.BlockSize())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(unpadded)), nil
}

// unpadPKCS7 removes PKCS#7 padding in constant time with respect to the
// padding bytes, so a decryption failure does not become a padding oracle.
func unpadPKCS7(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errShortPayload
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, errBadCipher
	}
	expected := make([]byte, padLen)
	for i := range expected {
		expected[i] = byte(padLen)
	}
	if subtle.ConstantTimeCompare(data[len(data)-padLen:], expected) != 1 {
		return nil, errBadCipher
	}
	return data[:len(data)-padLen], nil
}
