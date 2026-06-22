package mpt

// Hex-prefix (HP) encoding for compact storage of nibble paths.
// This is the Ethereum-specified encoding for Merkle-Patricia Trie paths.

// KeyToNibbles converts a byte slice to nibbles (each byte becomes two nibbles).
func KeyToNibbles(key []byte) []byte {
	if len(key) == 0 {
		return nil
	}
	nibbles := make([]byte, len(key)*2)
	for i, b := range key {
		nibbles[i*2] = b >> 4
		nibbles[i*2+1] = b & 0x0f
	}
	return nibbles
}

// NibblesToKey converts nibbles back to a byte slice.
// Panics if len(nibbles) is odd.
func NibblesToKey(nibbles []byte) []byte {
	if len(nibbles) == 0 {
		return nil
	}
	if len(nibbles)%2 != 0 {
		panic("nibbles must have even length for key conversion")
	}
	key := make([]byte, len(nibbles)/2)
	for i := 0; i < len(key); i++ {
		key[i] = nibbles[i*2]<<4 | nibbles[i*2+1]
	}
	return key
}

// CompactEncode applies hex-prefix encoding to a nibble slice.
//
// Flag byte format:
//   bit 0: 1 = leaf, 0 = extension
//   bit 1: 1 = odd-length nibbles, 0 = even-length nibbles
//   bits 2-7: if odd, nibble[0] goes here; if even, zero
//
// For even-length: [flag(0x00), nibble[0]|nibble[1]<<4, nibble[2]|nibble[3]<<4, ...]
// For odd-length:  [flag(0x10|nibble[0]), nibble[1]|nibble[2]<<4, ...]
func CompactEncode(nibbles []byte, isLeaf bool) []byte {
	if len(nibbles) == 0 {
		return []byte{0x00}
	}

	flag := byte(0x00)
	if isLeaf {
		flag |= 0x20
	}
	if len(nibbles)%2 == 0 {
		flag |= 0x00
	} else {
		flag |= 0x10
	}

	if len(nibbles)%2 == 0 {
		// Even length: flag byte + pairs
		result := make([]byte, 1+len(nibbles)/2)
		result[0] = flag
		for i := 0; i < len(nibbles); i += 2 {
			result[1+i/2] = nibbles[i]<<4 | nibbles[i+1]
		}
		return result
	} else {
		// Odd length: flag byte has first nibble in upper bits
		result := make([]byte, 1+len(nibbles)/2)
		result[0] = flag | nibbles[0]
		for i := 1; i < len(nibbles); i += 2 {
			result[1+i/2] = nibbles[i]<<4 | nibbles[i+1]
		}
		return result
	}
}

// CompactDecode reverses hex-prefix encoding.
// Returns (nibbles, isLeaf).
func CompactDecode(encoded []byte) ([]byte, bool) {
	if len(encoded) == 0 {
		return nil, false
	}

	flag := encoded[0]
	isLeaf := flag&0x20 != 0
	isOdd := flag&0x10 != 0

	var nibbles []byte
	if isOdd {
		nibbles = make([]byte, 2*len(encoded)-1)
		nibbles[0] = flag & 0x0f // lower nibble of flag is first nibble
		for i := 1; i < len(encoded); i++ {
			nibbles[2*i-1] = encoded[i] >> 4
			nibbles[2*i] = encoded[i] & 0x0f
		}
	} else {
		nibbles = make([]byte, 2*(len(encoded)-1))
		for i := 1; i < len(encoded); i++ {
			nibbles[2*(i-1)] = encoded[i] >> 4
			nibbles[2*(i-1)+1] = encoded[i] & 0x0f
		}
	}

	return nibbles, isLeaf
}

// CommonPrefix returns the length of the common prefix of two nibble slices.
func CommonPrefix(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
