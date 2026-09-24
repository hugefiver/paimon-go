package ast

import (
	"sync"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

func (n *Node) materializeUnlocked() error {
	if err := n.checkUnlocked(); err != nil {
		return err
	}
	if !n.isRawUnlocked() {
		if n.mu != nil {
			return n.loadChildrenUnlocked()
		}
		return nil
	}
	if n.typ == V_ARRAY || n.typ == V_OBJECT {
		n.lazyPos = 1
		n.loaded = true
		if n.mu != nil {
			return n.loadChildrenUnlocked()
		}
		return nil
	}
	parsed, code := parseRawToNodeLocal(n.raw)
	if code != 0 {
		return n.failLoad(code)
	}
	assignLoaded(n, &parsed)
	return nil
}

func (n *Node) failLoad(err error) error {
	n.typ, n.loaded, n.raw, n.err, n.lazyPos = V_ERROR, true, "", err, 0
	return err
}

// loadNext appends one raw child. Concurrent-read nodes finish this operation
// for all immediate children while holding their initialization lock.
func (n *Node) loadNext() error {
	src, pos := n.raw, n.lazyPos
	if pos == 0 {
		return nil
	}
	pos = skipJSONSpaceString(src, pos)
	close := byte(']')
	if n.typ == V_OBJECT {
		close = '}'
	}
	if pos >= len(src) {
		return n.failLoad(nativetypes.ERR_EOF)
	}
	if src[pos] == close {
		n.lazyPos, n.raw = 0, ""
		return nil
	}
	key := ""
	if n.typ == V_OBJECT {
		if src[pos] != '"' {
			return n.failLoad(nativetypes.ERR_INVALID_CHAR)
		}
		kp := localParser{src: src, pos: pos}
		var code nativetypes.ParsingError
		key, code = kp.parseString()
		if code != 0 {
			return n.failLoad(code)
		}
		pos = skipJSONSpaceString(src, kp.pos)
		if pos >= len(src) {
			return n.failLoad(nativetypes.ERR_EOF)
		}
		if src[pos] != ':' {
			return n.failLoad(nativetypes.ERR_INVALID_CHAR)
		}
		pos = skipJSONSpaceString(src, pos+1)
	}
	start := pos
	if start >= len(src) {
		return n.failLoad(nativetypes.ERR_EOF)
	}
	end, ok := scanPreTargetValueEndString(src, start)
	if !ok {
		return n.failLoad(nativetypes.ERR_INVALID_CHAR)
	}
	child := newUncheckedRaw(src[start:end], SearchOptions{ConcurrentRead: n.mu != nil})
	pos = skipJSONSpaceString(src, end)
	if pos >= len(src) {
		return n.failLoad(nativetypes.ERR_EOF)
	}
	switch src[pos] {
	case close:
		n.lazyPos, n.raw = 0, ""
	case ',':
		pos++
		n.lazyPos = pos
		// A trailing comma is not an empty remaining container.
		next := skipJSONSpaceString(src, pos)
		if next < len(src) && src[next] == close {
			return n.failLoad(nativetypes.ERR_INVALID_CHAR)
		}
	default:
		return n.failLoad(nativetypes.ERR_INVALID_CHAR)
	}
	if n.typ == V_ARRAY {
		// A shallow Node copy can have already exposed this slot through
		// its own cursor. Reuse that slot so a retained child pointer keeps
		// referring to the value seen by both copies.
		if size := len(n.arr); size < cap(n.arr) {
			n.arr = n.arr[:size+1]
			if n.arr[size] == nil {
				n.arr[size] = &child
			}
		} else {
			n.arr = append(n.arr, &child)
		}
	} else {
		if size := len(n.obj); size < cap(n.obj) {
			n.obj = n.obj[:size+1]
			if n.obj[size] == nil {
				n.obj[size] = &Pair{Key: key, Value: child}
			}
		} else {
			n.obj = append(n.obj, &Pair{Key: key, Value: child})
		}
	}
	return nil
}

func (n *Node) loadChildrenUnlocked() error {
	for n.lazyPos != 0 {
		if err := n.loadNext(); err != nil {
			return err
		}
	}
	return n.checkUnlocked()
}

func (n *Node) loadChildren() error {
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	return n.finishLazy()
}

// finishLazy prepares an initialized container for serialization while allowing
// untouched raw nodes to retain their original spelling and whitespace.
func (n *Node) finishLazy() error {
	if n == nil {
		return nil
	}
	if n.mu != nil {
		n.mu.RLock()
		if n.lazyPos == 0 {
			err := n.checkUnlocked()
			n.mu.RUnlock()
			return err
		}
		n.mu.RUnlock()
		n.mu.Lock()
		defer n.mu.Unlock()
	}
	return n.loadChildrenUnlocked()
}

// Explicit Load runs before publication to concurrent readers. Protect already
// exposed descendants as well as new raw children, without parsing untouched
// subtrees. Node copies may share child slots, so visit each slot only once.
func (n *Node) enableConcurrentChildren() {
	seen := map[*Node]bool{n: true}
	var enable func(*Node)
	enable = func(child *Node) {
		if child == nil {
			return
		}
		if child.mu == nil {
			child.mu = &sync.RWMutex{}
		}
		if len(child.arr) == 0 && len(child.obj) == 0 {
			return
		}
		if seen[child] {
			return
		}
		seen[child] = true
		for _, descendant := range child.arr {
			enable(descendant)
		}
		for _, pair := range child.obj {
			if pair != nil {
				enable(&pair.Value)
			}
		}
	}
	for _, child := range n.arr {
		enable(child)
	}
	for _, pair := range n.obj {
		if pair != nil {
			enable(&pair.Value)
		}
	}
}
