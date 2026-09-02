package words

import (
	"strings"
	"unicode/utf8"
)

// initialisms maps a word, folded to lower case, to the way Go spells it.
//
// The list the language's own linters hold each other to, which is the point of
// taking it from them rather than writing one: an author whose tree runs revive
// or staticcheck already has SortedByID enforced on the code they wrote, and a
// generator writing SortedById into the same package is forge failing a gate it
// caused. Nothing else in the tree holds this set, which is why a field named
// Id or Api or Url used to produce a name the author could not fix.
//
// Not all of them are acronyms. gRPC and OAuth are spelled the way their own
// projects spell them, which is what a reader recognises, and the table is a
// table of fixed spellings rather than of capitalisations for that reason.
//
// UTF8 carries its digit because that is the whole name; oauth2 does not,
// because the 2 is a version rather than part of the word, and [canonical]
// reattaches it.
var initialisms = map[string]string{
	"ack":   "ACK",
	"acl":   "ACL",
	"api":   "API",
	"ascii": "ASCII",
	"cpu":   "CPU",
	"css":   "CSS",
	"db":    "DB",
	"dns":   "DNS",
	"eof":   "EOF",
	"grpc":  "gRPC",
	"guid":  "GUID",
	"html":  "HTML",
	"http":  "HTTP",
	"https": "HTTPS",
	"id":    "ID",
	"ip":    "IP",
	"json":  "JSON",
	"lhs":   "LHS",
	"oauth": "OAuth",
	"os":    "OS",
	"qps":   "QPS",
	"ram":   "RAM",
	"rhs":   "RHS",
	"rpc":   "RPC",
	"sla":   "SLA",
	"smtp":  "SMTP",
	"sql":   "SQL",
	"ssh":   "SSH",
	"tcp":   "TCP",
	"tls":   "TLS",
	"ttl":   "TTL",
	"udp":   "UDP",
	"ui":    "UI",
	"uid":   "UID",
	"uri":   "URI",
	"url":   "URL",
	"utf8":  "UTF8",
	"uuid":  "UUID",
	"vm":    "VM",
	"xml":   "XML",
	"xmpp":  "XMPP",
	"xsrf":  "XSRF",
	"xss":   "XSS",
}

// widest is the longest key the table holds, with room over it.
//
// A word longer than this cannot be an initialism, which is worth knowing
// because it is what lets the lookup fold the word into a buffer on the stack
// rather than allocating a lower-cased copy of every word forge ever asks
// about.
const widest = 8

// Initialism returns the way Go spells a word that has a fixed spelling, and
// whether the word is one of them.
//
// Asked of the word as it was written, in whatever case: id, Id and ID are one
// question and it has one answer. Exported so that a layer can ask — a layer
// that needs to know whether a field name is an initialism should not be
// carrying its own copy of the list to find out.
func Initialism(word string) (string, bool) {
	var buf [widest]byte

	held, fitted := fold(word, buf[:])
	if !fitted {
		return "", false
	}

	spelled, is := initialisms[string(held)]
	return spelled, is
}

// fold writes a word in lower case into a buffer, and reports whether it fitted
// and was the kind of word that could be in the table at all.
//
// ASCII only, because every entry is: a word with a letter outside it is not an
// initialism and there is nothing to be gained by lower-casing it to find that
// out.
func fold(word string, into []byte) ([]byte, bool) {
	if len(word) > len(into) {
		return nil, false
	}

	for i := range len(word) {
		b := word[i]
		if b >= utf8.RuneSelf {
			return nil, false
		}
		into[i] = small(b)
	}
	return into[:len(word)], true
}

// small returns an ASCII byte in lower case.
func small(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b - 'A' + 'a'
	}
	return b
}

// canonical returns the way Go spells one word of an identifier, and whether
// that spelling came from the table rather than from the word itself.
//
// Three shapes reach the table and one does not. A word that is an initialism
// is spelled the way the table spells it. A word that is an initialism with a
// version after it — oauth2, and nothing else so far — is spelled by putting
// the digits back, because the digits are not part of the word. A word that is
// an initialism made plural takes the table's spelling and keeps the s, so that
// IDs survives a round trip through [Words] and [Join] rather than becoming
// IDS or Ids.
//
// Everything else is the author's own word and is returned exactly as it was
// written, and reported as not fixed so that the caller may recase its first
// letter. Lowering ADDRESS to Address would be forge deciding it knows better
// than a field name somebody typed, which it does not: the case a word is
// written in is the author's, and only its first letter belongs to the join.
func canonical(w string) (string, bool) {
	if spelled, is := Initialism(w); is {
		return spelled, true
	}

	if stem, digits := splitDigits(w); digits != "" {
		if spelled, is := Initialism(stem); is {
			return spelled + digits, true
		}
	}

	if stem, plural := strings.CutSuffix(w, "s"); plural {
		if spelled, is := Initialism(stem); is {
			return spelled + "s", true
		}
	}

	return w, false
}

// splitDigits takes the run of digits off the end of a word, and returns the
// word and the digits it lost.
func splitDigits(w string) (string, string) {
	at := len(w)
	for at > 0 && w[at-1] >= '0' && w[at-1] <= '9' {
		at--
	}
	return w[:at], w[at:]
}
