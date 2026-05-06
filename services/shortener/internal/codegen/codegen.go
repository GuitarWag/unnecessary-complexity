// Package codegen produces short URL codes from a base62 alphabet.
package codegen

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ErrInvalidLength is returned when a non-positive length is requested.
var ErrInvalidLength = errors.New("codegen: length must be > 0")

// Generator emits random base62 codes of a fixed length.
type Generator struct {
	length int
	reader io.Reader
}

// NewGenerator returns a Generator producing codes of the given length.
// If reader is nil, crypto/rand.Reader is used.
func NewGenerator(length int, reader io.Reader) (*Generator, error) {
	if length <= 0 {
		return nil, ErrInvalidLength
	}
	if reader == nil {
		reader = rand.Reader
	}
	return &Generator{length: length, reader: reader}, nil
}

// Generate returns a fresh base62 code.
func (g *Generator) Generate() (string, error) {
	buf := make([]byte, g.length)
	if _, err := io.ReadFull(g.reader, buf); err != nil {
		return "", fmt.Errorf("codegen: read random bytes: %w", err)
	}
	out := make([]byte, g.length)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
