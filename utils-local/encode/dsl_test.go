package encode

import (
	"testing"
)

func TestParser(t *testing.T) {
	println(DSLParserToString("b64de|AFoAAAEAAAABNgEsAAAIAH//fwgAAAABACAAOgAAAAAAAAAAAAAAAAAAAAA05gAAAAEAAAAAAAAAAChDT05ORUNUX0RBVEE9KENPTU1BTkQ9dmVyc2lvbikp"))
}

func TestSimhash(t *testing.T) {
	s1 := "sdafjdsakfjklawjflkwejf"
	s2 := "asdfjalkjflkewjflkqj"

	sim1 := Simhash([]byte(s1))
	sim2 := Simhash([]byte(s2))

	println(SimhashCompare(sim1, sim2))
}
