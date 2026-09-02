package words_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/internal/words"
)

func TestWhereAnIdentifiersWordsEnd(t *testing.T) {
	for _, one := range []struct {
		name string
		want []string
	}{
		{"UserIDToken", []string{"User", "ID", "Token"}},
		{"userId", []string{"user", "Id"}},
		{"http_server", []string{"http", "server"}},
		{"oauth2Token", []string{"oauth2", "Token"}},
		{"JSONValue", []string{"JSON", "Value"}},
		{"StatusOK", []string{"Status", "OK"}},
		{"ID", []string{"ID"}},
		{"ABC", []string{"ABC"}},
		{"HTTPServer", []string{"HTTP", "Server"}},

		// A run of capitals that spells an initialism keeps the s that makes it
		// plural, which is the one place the ordinary rule reads UserIDs as
		// User, I and Ds.
		{"UserIDs", []string{"User", "IDs"}},
		{"IDs", []string{"IDs"}},
		{"URLs", []string{"URLs"}},

		// And a run that does not spell one does not: ABCs is not a word, so
		// the last capital opens the next.
		{"ABCs", []string{"AB", "Cs"}},

		{"", nil},
		{"__", nil},
		{"a", []string{"a"}},
		{"Ünïcode", []string{"Ünïcode"}},
	} {
		if got := words.Words(one.name); !slices.Equal(got, one.want) {
			t.Errorf("Words(%q) = %q, want %q", one.name, got, one.want)
		}
	}
}

func TestJoiningWordsIntoAnExportedName(t *testing.T) {
	for _, one := range []struct {
		parts []string
		want  string
	}{
		{[]string{"user", "id"}, "UserID"},
		{[]string{"userId"}, "UserID"},
		{[]string{"api", "url"}, "APIURL"},
		{[]string{"json", "value"}, "JSONValue"},
		{[]string{"http_server"}, "HTTPServer"},
		{[]string{"uuid"}, "UUID"},
		{[]string{"oauth2Token"}, "OAuth2Token"},
		{[]string{"ids"}, "IDs"},
		{[]string{"New", "Persons"}, "NewPersons"},

		// gRPC is spelled with a small g by the project that owns it, and
		// gRPCClient is not a name a package can export.
		{[]string{"grpc", "client"}, "GRPCClient"},
		{[]string{"client", "grpc"}, "ClientgRPC"},

		{nil, ""},
		{[]string{""}, ""},
	} {
		if got := words.Join(one.parts...); got != one.want {
			t.Errorf("Join(%q) = %q, want %q", one.parts, got, one.want)
		}
	}
}

func TestExportIsTheJoinOfOneName(t *testing.T) {
	for name, want := range map[string]string{
		"userId":      "UserID",
		"apiUrl":      "APIURL",
		"jsonValue":   "JSONValue",
		"http_server": "HTTPServer",
		"uuid":        "UUID",
		"oauth2Token": "OAuth2Token",
	} {
		if got := words.Export(name); got != want {
			t.Errorf("Export(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTheGoInitialismSet(t *testing.T) {
	for _, one := range []struct {
		word    string
		spelled string
		is      bool
	}{
		{"id", "ID", true},
		{"Id", "ID", true},
		{"ID", "ID", true},
		{"grpc", "gRPC", true},
		{"oauth", "OAuth", true},
		{"person", "", false},
	} {
		spelled, is := words.Initialism(one.word)
		if is != one.is || spelled != one.spelled {
			t.Errorf("Initialism(%q) = %q, %v; want %q, %v", one.word, spelled, is, one.spelled, one.is)
		}
	}
}
