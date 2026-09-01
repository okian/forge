// Package other holds types a subject reaches across a package boundary.
//
// The distinction it exists for is not about what the types are but about where
// they are: a codec that names one has to bind the package it comes from, and a
// file that binds nothing names an identifier that is not there.
package other

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Celsius is a named type over a basic one, declared somewhere else.
type Celsius float64

// Label is the same again over a string.
type Label string

// Place is a struct from another package, which gets a codec of its own.
type Place struct {
	City string
}

// Ticks carries a codec of its own, so a member of this type is written by
// calling a method on it — which names no package at all.
type Ticks int64

// MarshalJSONTo writes the ticks as a number.
func (t Ticks) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Int(int64(t)))
}

// UnmarshalJSONFrom reads them back.
func (t *Ticks) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return json.UnmarshalDecode(dec, (*int64)(t))
}

// Blob is bytes, which go to the reflective encoder rather than through a
// spelling of their own.
type Blob []byte
