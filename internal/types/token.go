package types

import (
	"math/big"
)

// TokenKind identifies which token (APT or NPT).
type TokenKind uint8

const (
	TokenAPT TokenKind = iota
	TokenNPT
)

// String returns the string representation of the token kind.
func (tk TokenKind) String() string {
	switch tk {
	case TokenAPT:
		return "APT"
	case TokenNPT:
		return "NPT"
	default:
		return "UNKNOWN"
	}
}

// TokenAmount wraps a *big.Int with token arithmetic safety.
// All values are in the smallest unit (wei-equivalent), with 18 decimals.
type TokenAmount struct {
	inner *big.Int
}

// Token precision constants.
var (
	APTPrecision = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	NPTPrecision = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	ZeroAmount   = TokenAmount{inner: new(big.Int)}
)

// NewTokenAmount creates a TokenAmount from a big.Int.
func NewTokenAmount(v *big.Int) TokenAmount {
	if v == nil {
		return TokenAmount{inner: new(big.Int)}
	}
	return TokenAmount{inner: new(big.Int).Set(v)}
}

// NewTokenAmountUint64 creates a TokenAmount from a uint64 (in smallest unit).
func NewTokenAmountUint64(v uint64) TokenAmount {
	return TokenAmount{inner: new(big.Int).SetUint64(v)}
}

// NewTokenAmountFromTokens creates a TokenAmount from a whole token count
// (e.g., NewTokenAmountFromTokens(100) = 100 * 10^18 wei).
func NewTokenAmountFromTokens(tokens int64) *TokenAmount {
	wei := new(big.Int).Mul(big.NewInt(tokens), APTPrecision)
	return &TokenAmount{inner: wei}
}

// Add returns the sum of two TokenAmounts.
func (t TokenAmount) Add(other TokenAmount) TokenAmount {
	return TokenAmount{inner: new(big.Int).Add(t.inner, other.inner)}
}

// Sub returns the difference of two TokenAmounts (panics if negative).
func (t TokenAmount) Sub(other TokenAmount) TokenAmount {
	if t.Cmp(other) < 0 {
		panic("TokenAmount underflow")
	}
	return TokenAmount{inner: new(big.Int).Sub(t.inner, other.inner)}
}

// Mul returns the product with a scalar.
func (t TokenAmount) Mul(scalar *big.Int) TokenAmount {
	return TokenAmount{inner: new(big.Int).Mul(t.inner, scalar)}
}

// Div returns the quotient with a scalar.
func (t TokenAmount) Div(scalar *big.Int) TokenAmount {
	return TokenAmount{inner: new(big.Int).Div(t.inner, scalar)}
}

// Cmp compares two TokenAmounts. Returns -1, 0, or 1.
func (t TokenAmount) Cmp(other TokenAmount) int {
	return t.inner.Cmp(other.inner)
}

// IsZero returns true if the amount is zero.
func (t TokenAmount) IsZero() bool {
	if t.inner == nil {
		return true
	}
	return t.inner.Sign() == 0
}

// IsNil returns true if the inner big.Int is nil (not initialized).
func (t TokenAmount) IsNil() bool {
	return t.inner == nil
}

// IsNegative returns true if the amount is negative.
func (t TokenAmount) IsNegative() bool {
	return t.inner.Sign() < 0
}

// ToBigInt returns the underlying big.Int.
func (t TokenAmount) ToBigInt() *big.Int {
	return new(big.Int).Set(t.inner)
}

// Uint64 returns the amount as uint64. Panics if overflow.
func (t TokenAmount) Uint64() uint64 {
	if !t.inner.IsUint64() {
		panic("TokenAmount overflow for uint64")
	}
	return t.inner.Uint64()
}

// String returns the string representation.
func (t TokenAmount) String() string {
	return t.inner.String()
}

// Set sets the value from another TokenAmount.
func (t *TokenAmount) Set(other TokenAmount) {
	if t.inner == nil {
		t.inner = new(big.Int)
	}
	t.inner.Set(other.inner)
}

// Clone returns a deep copy.
func (t TokenAmount) Clone() TokenAmount {
	return TokenAmount{inner: new(big.Int).Set(t.inner)}
}

// AddTo mutates t by adding other.
func (t *TokenAmount) AddTo(other TokenAmount) {
	t.inner.Add(t.inner, other.inner)
}

// SubTo mutates t by subtracting other. Panics on underflow.
func (t *TokenAmount) SubTo(other TokenAmount) {
	if t.Cmp(other) < 0 {
		panic("TokenAmount underflow")
	}
	t.inner.Sub(t.inner, other.inner)
}
