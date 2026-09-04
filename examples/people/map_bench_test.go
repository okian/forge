package people

import "testing"

// The fused writer's whole point: the document, no Person built. Zero
// allocations once the buffer is warm, which the budget gate holds it to.
func BenchmarkFusedJSONEncode(b *testing.B) {
	src := applicantFixture()

	var buf []byte
	write := func() {
		var err error
		if buf, err = AppendPersonJSONFromApplicant(buf[:0], &src); err != nil {
			b.Fatal(err)
		}
	}
	write()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		write()
	}
}

// The same document by way of the value, which is what the fusion saves: the
// Person is built, then encoded, and whatever building costs shows here.
func BenchmarkConstructThenEncode(b *testing.B) {
	src := applicantFixture()

	var buf []byte
	write := func() {
		held := PersonFromApplicant(&src)
		var err error
		if buf, err = held.AppendJSON(buf[:0]); err != nil {
			b.Fatal(err)
		}
	}
	write()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		write()
	}
}
