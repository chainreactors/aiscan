package encode

import "strings"

func DSLParserToString(s string) (string, bool) {
	bs, ok := DSLParser(s)
	return string(bs), ok
}

func DSLParser(s string) ([]byte, bool) {
	var bs []byte
	var operator, content string

	if i := strings.Index(s, "|"); i > 0 {
		operator = s[:i]
		content = s[i+1:]
	} else {
		return []byte(s), false
	}

	switch operator {
	case "b64de":
		bs = Base64Decode(content)
	case "b64en":
		bs = []byte(Base64Encode([]byte(content)))
	case "unhex":
		bs = HexDecode(content)
	case "hex":
		bs = []byte(HexEncode([]byte(content)))
	case "md5":
		bs = []byte(Md5Hash([]byte(content)))
	default:
		return []byte(content), false
	}
	return bs, true
}
