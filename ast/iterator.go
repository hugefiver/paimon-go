package ast

// Iterator is the shared cursor state for ListIterator and ObjectIterator.
// The zero value is an empty iterator.
type Iterator struct {
	pos  int
	node *Node
}

// HasNext reports whether the iterator has more elements.
func (it *Iterator) HasNext() bool {
	if it == nil || it.node == nil {
		return false
	}
	for it.pos >= it.Len() && it.node.lazyPos != 0 {
		if err := it.node.loadNext(); err != nil {
			return false
		}
	}
	return it.pos < it.Len()
}

// Len returns the current number of children in the iterator's parent node.
func (it *Iterator) Len() int {
	if it == nil || it.node == nil {
		return 0
	}
	switch it.node.typ {
	case V_ARRAY:
		return len(it.node.arr)
	case V_OBJECT:
		return len(it.node.obj)
	default:
		return 0
	}
}

// Pos returns the cursor position, i.e. the number of elements already
// consumed via Next.
func (it *Iterator) Pos() int {
	if it == nil {
		return 0
	}
	return it.pos
}

// ListIterator iterates over the children of an array node.
type ListIterator struct {
	Iterator
}

// HasNext reports whether the iterator has more array elements.
func (it *ListIterator) HasNext() bool {
	if it == nil || it.node == nil {
		return false
	}
	for it.pos >= it.Len() && it.node.lazyPos != 0 {
		if err := it.node.loadNext(); err != nil {
			return false
		}
	}
	return it.pos < it.Len()
}

// Len returns the current number of elements in the iterator's parent array.
func (it *ListIterator) Len() int {
	if it == nil || it.node == nil || it.node.typ != V_ARRAY {
		return 0
	}
	return len(it.node.arr)
}

// Next advances the iterator and copies the next element into v.
// It returns false when the iterator is exhausted.
func (it *ListIterator) Next(v *Node) bool {
	for it != nil && it.HasNext() {
		n := it.node.arr[it.pos].snapshot()
		it.pos++
		if n.typ != V_NONE && n.typ != V_ERROR {
			*v = n
			return true
		}
	}
	return false
}

// ObjectIterator iterates over the pairs of an object node.
type ObjectIterator struct {
	Iterator
}

// HasNext reports whether the iterator has more object pairs.
func (it *ObjectIterator) HasNext() bool {
	if it == nil || it.node == nil {
		return false
	}
	for it.pos >= it.Len() && it.node.lazyPos != 0 {
		if err := it.node.loadNext(); err != nil {
			return false
		}
	}
	return it.pos < it.Len()
}

// Len returns the current number of pairs in the iterator's parent object.
func (it *ObjectIterator) Len() int {
	if it == nil || it.node == nil || it.node.typ != V_OBJECT {
		return 0
	}
	return len(it.node.obj)
}

// Next advances the iterator and copies the next pair into p.
// It returns false when the iterator is exhausted.
func (it *ObjectIterator) Next(p *Pair) bool {
	for it != nil && it.HasNext() {
		n := it.node.obj[it.pos]
		it.pos++
		if n == nil {
			continue
		}
		value := n.Value.snapshot()
		if value.typ != V_NONE && value.typ != V_ERROR {
			*p = Pair{Key: n.Key, Value: value}
			return true
		}
	}
	return false
}
