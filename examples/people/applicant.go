package people

// Applicant carries the same facts as [Person], spelled the way the intake
// form spells them: most members match by name, and the one that does not is
// pinned by the from tag on [Person.Email].
type Applicant struct {
	ID      int
	Name    string
	Contact string
	Age     int
	Aliases []string
}
