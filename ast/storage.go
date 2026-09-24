package ast

// Adopt parser-owned backing storage without copying the Node or Pair values.
// The pointer index can grow and shrink independently of the stable child slots.
func newArrayOwned(values []Node) Node {
	items := make([]*Node, len(values))
	for i := range values {
		items[i] = &values[i]
	}
	return Node{typ: V_ARRAY, exists: true, loaded: true, arr: items}
}

func newObjectOwned(values []Pair) Node {
	items := make([]*Pair, len(values))
	for i := range values {
		items[i] = &values[i]
	}
	return Node{typ: V_OBJECT, exists: true, loaded: true, obj: items}
}

func (n *Node) removeArrayAt(i int) {
	*n.arr[i] = Node{}
	copy(n.arr[i:], n.arr[i+1:])
	n.arr[len(n.arr)-1] = nil
	n.arr = n.arr[:len(n.arr)-1]
}

func (n *Node) removeObjectAt(i int) {
	*n.obj[i] = Pair{}
	copy(n.obj[i:], n.obj[i+1:])
	n.obj[len(n.obj)-1] = nil
	n.obj = n.obj[:len(n.obj)-1]
}

// SortKeys reorders values in stable slots, as Sonic's linked storage does.
type pairSlots []*Pair

func (p pairSlots) Len() int           { return len(p) }
func (p pairSlots) Less(i, j int) bool { return p[i].Key < p[j].Key }
func (p pairSlots) Swap(i, j int)      { *p[i], *p[j] = *p[j], *p[i] }
