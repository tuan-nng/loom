package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
)

func nullToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func ptrToNull(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// NewID returns an opaque 32-hex-char row ID: 16 random bytes from
// crypto/rand (ADR-001 §3.3). IDs carry no ordering property.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("store: crypto/rand failure: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
