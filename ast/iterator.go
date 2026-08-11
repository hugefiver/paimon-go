package ast

// Iterator is the shared cursor state for ListIterator and ObjectIterator.
// The zero value is an empty iterator.
type Iterator struct {
	pos    int
	length int
}

// HasNext reports whether the iterator has more elements.
func (it *Iterator) HasNext() bool {
	return it != nil && it.pos < it.length
}

// Len returns the total number of elements the iterator was created with.
func (it *Iterator) Len() int {
	if it == nil {
		return 0
	}
	return it.length
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
	values []Node
}

// Next advances the iterator and copies the next element into v.
// It returns false when the iterator is exhausted.
func (it *ListIterator) Next(v *Node) bool {
	if it == nil || !it.HasNext() {
		return false
	}
	*v = it.values[it.pos]
	it.pos++
	return true
}

// ObjectIterator iterates over the pairs of an object node.
type ObjectIterator struct {
	Iterator
	pairs []Pair
}

// Next advances the iterator and copies the next pair into p.
// It returns false when the iterator is exhausted.
func (it *ObjectIterator) Next(p *Pair) bool {
	if it == nil || !it.HasNext() {
		return false
	}
	*p = it.pairs[it.pos]
	it.pos++
	return true
}
