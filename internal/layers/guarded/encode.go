package guarded

import "strings"

// encoding writes the container's JSON codec on the guarded type.
//
// Written here rather than wrapped from beneath. A codec over a container is
// written out of the walk, and this layer took the walk off the stack, so the
// layer that writes codecs saw a stack with nothing to walk and wrote none —
// which is what it says it does, and which leaves the codec for the whole
// container to whoever withdrew the walk. That is this.
//
// Which of the two ways it is written is the one thing about a lock a caller
// might reasonably want to decide, and it is the only option here that changes
// what runs rather than what exists.
func (p plan) encoding(w *strings.Builder) {
	if p.locked {
		p.encodingLocked(w)
	} else {
		p.encodingSnapshot(w)
	}
	p.encodingMarshal(w)
}

// encodingSnapshot copies first, so that nothing holds the lock for as long as
// a write takes.
func (p plan) encodingSnapshot(w *strings.Builder) {
	w.WriteString("// " + appendMethod + " appends the container to dst as a JSON array, and returns\n")
	w.WriteString("// the extended buffer.\n")
	w.WriteString("//\n")
	w.WriteString("// The elements are copied under the read lock and the copy is what is\n")
	w.WriteString("// written, so the lock is held for a copy rather than for a write. That\n")
	w.WriteString("// matters because the buffer may be flushed to a writer between calls: a\n")
	w.WriteString("// socket that stopped reading would otherwise hold this container against\n")
	w.WriteString("// every writer for as long as it took to time out.\n")
	w.WriteString("//\n")
	w.WriteString("// It costs one copy of the elements per document. A caller who owns their\n")
	w.WriteString("// writer and would rather not pay it can ask for the lock to be held\n")
	w.WriteString("// instead.\n")
	w.WriteString("func (" + p.receiver + " *" + p.declared + ") " + appendMethod +
		"(dst []byte) ([]byte, error) {\n")
	w.WriteString("\theld := " + p.receiver + "." + snapshot + "()\n\n")
	w.WriteString("\tvar err error\n")
	w.WriteString("\tmark := len(dst)\n")
	w.WriteString("\tfor i := range held {\n")
	w.WriteString("\t\tdst = append(dst, ',')\n")
	w.WriteString("\t\tif dst, err = held[i]." + appendMethod + "(dst); err != nil {\n")
	w.WriteString("\t\t\treturn dst, err\n")
	w.WriteString("\t\t}\n")
	w.WriteString("\t}\n")
	w.WriteString("\tif len(dst) == mark {\n")
	w.WriteString("\t\treturn append(dst, '[', ']'), nil\n")
	w.WriteString("\t}\n")
	w.WriteString("\tdst[mark] = '['\n")
	w.WriteString("\treturn append(dst, ']'), nil\n")
	w.WriteString("}\n\n")
}

// encodingLocked writes straight from behind the lock, for a declaration that
// asked for it.
func (p plan) encodingLocked(w *strings.Builder) {
	w.WriteString("// " + appendMethod + " appends the container to dst as a JSON array, with the\n")
	w.WriteString("// read lock held for the length of the write.\n")
	w.WriteString("//\n")
	w.WriteString("// Written this way because the declaration asked for it, and it is the\n")
	w.WriteString("// right answer only when nothing slow happens to the buffer meanwhile:\n")
	w.WriteString("// nothing is copied, and every writer of this container waits for however\n")
	w.WriteString("// long the append takes to finish.\n")
	w.WriteString("func (" + p.receiver + " *" + p.declared + ") " + appendMethod +
		"(dst []byte) ([]byte, error) {\n")
	w.WriteString("\t" + p.receiver + "." + lockField + ".RLock()\n")
	w.WriteString("\tdefer " + p.receiver + "." + lockField + ".RUnlock()\n\n")
	w.WriteString("\tvar err error\n")
	w.WriteString("\tmark := len(dst)\n")
	w.WriteString("\tfor v := range " + p.receiver + "." + heldField + "." + walkMethod + "() {\n")
	w.WriteString("\t\tdst = append(dst, ',')\n")
	w.WriteString("\t\tif dst, err = v." + appendMethod + "(dst); err != nil {\n")
	w.WriteString("\t\t\treturn dst, err\n")
	w.WriteString("\t\t}\n")
	w.WriteString("\t}\n")
	w.WriteString("\tif len(dst) == mark {\n")
	w.WriteString("\t\treturn append(dst, '[', ']'), nil\n")
	w.WriteString("\t}\n")
	w.WriteString("\tdst[mark] = '['\n")
	w.WriteString("\treturn append(dst, ']'), nil\n")
	w.WriteString("}\n\n")
}

// encodingMarshal writes the entry point the standard library dispatches to,
// which reaches the appender either way.
//
// A fresh buffer rather than a borrowed one, because this layer emits no
// runtime to borrow from: the appender is the fast path, and a caller who
// cares which buffer is written into calls it directly.
func (p plan) encodingMarshal(w *strings.Builder) {
	w.WriteString("// " + marshalMethod + " writes the container as a compact JSON array.\n")
	w.WriteString("//\n")
	w.WriteString("// It is what the standard library dispatches to. A caller who already\n")
	w.WriteString("// holds a buffer appends with " + appendMethod + " instead, which is the\n")
	w.WriteString("// same document without the allocation.\n")
	w.WriteString("func (" + p.receiver + " *" + p.declared + ") " + marshalMethod +
		"() ([]byte, error) {\n")
	w.WriteString("\treturn " + p.receiver + "." + appendMethod + "(nil)\n")
	w.WriteString("}\n\n")
}
