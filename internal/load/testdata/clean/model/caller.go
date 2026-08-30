package model

// Record appends a person to the collection. Push and Len are generated, and
// nothing has generated them yet: this file is the reason function bodies are
// stripped before type-checking.
func Record(people *Persons, p Person) int {
	people.Push(p)
	return people.Len()
}
