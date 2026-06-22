package types

import (
	"encoding/hex"
	"errors"
)

const AddressLength = 20

// Address represents a 20-byte account address.
type Address [AddressLength]byte

// Predefined addresses.
var (
	ZeroAddress    = Address{}
	AMMPoolAddress = Address{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
	}
)

// Hex returns the hex string representation of the address.
func (a Address) Hex() string {
	return hex.EncodeToString(a[:])
}

// Bytes returns the byte slice representation.
func (a Address) Bytes() []byte {
	b := make([]byte, AddressLength)
	copy(b, a[:])
	return b
}

// IsZero returns true if the address is the zero address.
func (a Address) IsZero() bool {
	return a == ZeroAddress
}

// String returns the hex string (implements fmt.Stringer).
func (a Address) String() string {
	return a.Hex()
}

// HexToAddress decodes a hex string into an Address.
func HexToAddress(s string) (Address, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return Address{}, err
	}
	if len(b) > AddressLength {
		return Address{}, errors.New("hex string too long for address")
	}
	var addr Address
	copy(addr[AddressLength-len(b):], b)
	return addr, nil
}

// BytesToAddress converts a byte slice to an Address.
func BytesToAddress(b []byte) (Address, error) {
	if len(b) > AddressLength {
		return Address{}, errors.New("byte slice too long for address")
	}
	var addr Address
	copy(addr[AddressLength-len(b):], b)
	return addr, nil
}
