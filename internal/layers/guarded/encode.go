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
		return
	}
	p.encodingSnapshot(w)
}

// encodingSnapshot copies first, so that nothing holds the lock for as long as
// a write takes.
func (p plan) encodingSnapshot(w *strings.Builder) {
	w.WriteString("// " + marshalMethod + " writes the container as a JSON array.\n")
	w.WriteString("//\n")
	w.WriteString("// The elements are copied under the read lock and the copy is what is\n")
	w.WriteString("// written, so the lock is held for a copy rather than for a write. That\n")
	w.WriteString("// matters because the encoder's writer is the caller's: a socket that\n")
	w.WriteString("// stopped reading would otherwise hold this container against every writer\n")
	w.WriteString("// for as long as it took to time out.\n")
	w.WriteString("//\n")
	w.WriteString("// It costs one copy of the elements per document. A caller who owns their\n")
	w.WriteString("// writer and would rather not pay it can ask for the lock to be held\n")
	w.WriteString("// instead.\n")
	w.WriteString("func (" + p.receiver + " *" + p.declared + ") " + marshalMethod +
		"(enc *jsontext.Encoder) error {\n")
	w.WriteString("\theld := " + p.receiver + "." + snapshot + "()\n\n")
	w.WriteString("\tif err := enc.WriteToken(jsontext.BeginArray); err != nil {\n")
	w.WriteString("\t\treturn err\n")
	w.WriteString("\t}\n")
	w.WriteString("\tfor i := range held {\n")
	w.WriteString("\t\tif err := held[i]." + marshalMethod + "(enc); err != nil {\n")
	w.WriteString("\t\t\treturn err\n")
	w.WriteString("\t\t}\n")
	w.WriteString("\t}\n")
	w.WriteString("\treturn enc.WriteToken(jsontext.EndArray)\n")
	w.WriteString("}\n\n")
}

// encodingLocked writes straight from behind the lock, for a declaration that
// asked for it.
func (p plan) encodingLocked(w *strings.Builder) {
	w.WriteString("// " + marshalMethod + " writes the container as a JSON array, with the read\n")
	w.WriteString("// lock held for the length of the write.\n")
	w.WriteString("//\n")
	w.WriteString("// Written this way because the declaration asked for it, and it is the\n")
	w.WriteString("// right answer only when the encoder's writer is one the caller controls:\n")
	w.WriteString("// nothing is copied, and every writer of this container waits for however\n")
	w.WriteString("// long the encoder takes to finish. A writer that blocks blocks them all.\n")
	w.WriteString("func (" + p.receiver + " *" + p.declared + ") " + marshalMethod +
		"(enc *jsontext.Encoder) error {\n")
	w.WriteString("\t" + p.receiver + "." + lockField + ".RLock()\n")
	w.WriteString("\tdefer " + p.receiver + "." + lockField + ".RUnlock()\n\n")
	w.WriteString("\tif err := enc.WriteToken(jsontext.BeginArray); err != nil {\n")
	w.WriteString("\t\treturn err\n")
	w.WriteString("\t}\n")
	w.WriteString("\tfor v := range " + p.receiver + "." + heldField + "." + walkMethod + "() {\n")
	w.WriteString("\t\tif err := v." + marshalMethod + "(enc); err != nil {\n")
	w.WriteString("\t\t\treturn err\n")
	w.WriteString("\t\t}\n")
	w.WriteString("\t}\n")
	w.WriteString("\treturn enc.WriteToken(jsontext.EndArray)\n")
	w.WriteString("}\n\n")
}
