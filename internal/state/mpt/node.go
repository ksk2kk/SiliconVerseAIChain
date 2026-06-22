package mpt

import "fmt"

// HashValue represents a 32-byte node hash.
type HashValue [32]byte

// nodeKind identifies the type of trie node.
type nodeKind uint8

const (
	kindNil       nodeKind = iota
	kindLeaf
	kindExtension
	kindBranch
	kindHash
)

// node is the internal interface for trie nodes.
type node interface {
	kind() nodeKind
	hash() HashValue
	dirty() bool
	setDirty(bool)
	cachedHash() (HashValue, bool)
	fstring(string) string
}

// ---- Concrete node types ----

// nilNode represents an empty node (no value).
type nilNode struct{}

func (n *nilNode) kind() nodeKind              { return kindNil }
func (n *nilNode) hash() HashValue             { return EmptyNodeHash }
func (n *nilNode) dirty() bool                 { return false }
func (n *nilNode) setDirty(bool)               {}
func (n *nilNode) cachedHash() (HashValue, bool) { return EmptyNodeHash, true }
func (n *nilNode) fstring(indent string) string { return indent + "<nil>" }

func (n *leafNode) fstring(indent string) string {
	return indent + "<leaf> key=" + string(n.KeySuffix) + " val=" + string(n.Value)
}
func (n *extensionNode) fstring(indent string) string {
	return indent + "<ext> prefix=" + string(n.Prefix)
}
func (n *branchNode) fstring(indent string) string {
	return indent + "<branch> children=" + fmt.Sprintf("%d", n.hasChildren())
}
func (n *hashNode) fstring(indent string) string {
	return indent + "<hash> " + fmt.Sprintf("%x", n.HashValue[:4])
}

// leafNode stores a key suffix and value.
type leafNode struct {
	KeySuffix []byte // Remaining nibble path (uncompressed)
	Value     []byte // Stored value
	hashCache HashValue
	dirtyFlag bool
	hashValid bool
}

func (n *leafNode) kind() nodeKind              { return kindLeaf }
func (n *leafNode) hash() HashValue             { return n.hashCache }
func (n *leafNode) dirty() bool                 { return n.dirtyFlag }
func (n *leafNode) setDirty(d bool)             { n.dirtyFlag = d; n.hashValid = false }
func (n *leafNode) cachedHash() (HashValue, bool) { return n.hashCache, n.hashValid }

// extensionNode stores a shared prefix and a pointer to the next node.
type extensionNode struct {
	Prefix    []byte // Shared nibble path (uncompressed)
	Next      node   // Child node
	hashCache HashValue
	dirtyFlag bool
	hashValid bool
}

func (n *extensionNode) kind() nodeKind              { return kindExtension }
func (n *extensionNode) hash() HashValue             { return n.hashCache }
func (n *extensionNode) dirty() bool                 { return n.dirtyFlag }
func (n *extensionNode) setDirty(d bool)             { n.dirtyFlag = d; n.hashValid = false }
func (n *extensionNode) cachedHash() (HashValue, bool) { return n.hashCache, n.hashValid }

// branchNode stores up to 16 children (one per nibble) and an optional value.
type branchNode struct {
	Children  [16]node // indexed by nibble 0-15
	Value     []byte   // value at this node (if any)
	hashCache HashValue
	dirtyFlag bool
	hashValid bool
}

func (n *branchNode) kind() nodeKind              { return kindBranch }
func (n *branchNode) hash() HashValue             { return n.hashCache }
func (n *branchNode) dirty() bool                 { return n.dirtyFlag }
func (n *branchNode) setDirty(d bool)             { n.dirtyFlag = d; n.hashValid = false }
func (n *branchNode) cachedHash() (HashValue, bool) { return n.hashCache, n.hashValid }

// hasChildren returns the number of non-nil children.
func (n *branchNode) hasChildren() int {
	count := 0
	for _, child := range n.Children {
		if child != nil && child.kind() != kindNil {
			count++
		}
	}
	return count
}

// hashNode is a placeholder referencing a node stored in the database by hash.
type hashNode struct {
	HashValue HashValue
}

func (n *hashNode) kind() nodeKind                { return kindHash }
func (n *hashNode) hash() HashValue               { return n.HashValue }
func (n *hashNode) dirty() bool                   { return false }
func (n *hashNode) setDirty(bool)                 {}
func (n *hashNode) cachedHash() (HashValue, bool) { return n.HashValue, true }

// EmptyNodeHash is the hash of an empty trie.
var EmptyNodeHash = HashValue{}
