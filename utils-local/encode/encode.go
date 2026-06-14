package encode

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/go-dedup/simhash"
	"github.com/twmb/murmur3"
	"io/ioutil"
	"strconv"
	"strings"
)

func Base64Decode(s string) []byte {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return data
}

func Base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func XorEncode(bs []byte, keys []byte, cursor int) []byte {
	if len(keys) == 0 {
		return bs
	}

	newbs := make([]byte, len(bs))
	for i, b := range bs {
		newbs[i] = b ^ keys[(i+cursor)%len(keys)]
	}
	return newbs
}

func HexDecode(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func HexEncode(b []byte) string {
	return hex.EncodeToString(b)
}

func Md5Hash(raw []byte) string {
	m := md5.Sum(raw)
	return hex.EncodeToString(m[:])
}

func Mmh3Hash32(raw []byte) string {
	var h32 = murmur3.New32()
	_, _ = h32.Write(standBase64(raw))
	return fmt.Sprintf("%d", int32(h32.Sum32()))
}

func standBase64(braw []byte) []byte {
	bckd := base64.StdEncoding.EncodeToString(braw)
	var buffer bytes.Buffer
	for i := 0; i < len(bckd); i++ {
		ch := bckd[i]
		buffer.WriteByte(ch)
		if (i+1)%76 == 0 {
			buffer.WriteByte('\n')
		}
	}
	buffer.WriteByte('\n')
	return buffer.Bytes()
}

func Simhash(raw []byte) string {
	sh := simhash.NewSimhash()
	return fmt.Sprintf("%x", sh.GetSimhash(sh.NewWordFeatureSet(raw)))
}

func SimhashCompare(s, other string) uint8 {
	return simhash.Compare(parseHex(s), parseHex(other))
}

func parseHex(s string) uint64 {
	i, _ := strconv.ParseUint(s, 16, 64)
	return i
}

func MustDeflateCompress(input []byte) []byte {
	output, err := DeflateCompress(input)
	if err != nil {
		if !IsEOF(err) {
			panic(err)
		}
	}
	return output
}

func DeflateCompress(input []byte) ([]byte, error) {
	var bf = bytes.NewBuffer([]byte{})
	var flater, _ = flate.NewWriter(bf, flate.BestCompression)
	defer flater.Close()
	if _, err := flater.Write(input); err != nil {
		return []byte{}, err
	}
	if err := flater.Flush(); err != nil {
		return []byte{}, err
	}
	return bf.Bytes(), nil
}

func MustDeflateDeCompress(input []byte) []byte {
	output, err := DeflateDeCompress(input)
	if err != nil {
		if !IsEOF(err) {
			panic(err)
		}
	}
	return output
}

func DeflateDeCompress(input []byte) ([]byte, error) {
	rdata := bytes.NewReader(input)
	r := flate.NewReader(rdata)
	return ioutil.ReadAll(r)
}

func MustGzipCompress(input []byte) []byte {
	output, err := GzipCompress(input)
	if err != nil {
		if !IsEOF(err) {
			panic(err)
		}
	}
	return output
}

func GzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	_, err := gzipWriter.Write(data)
	if err != nil {
		return nil, err
	}

	err = gzipWriter.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func MustGzipDecompress(input []byte) []byte {
	output, err := GzipDecompress(input)
	if err != nil {
		if !IsEOF(err) {
			panic(err)
		}
	}
	return output
}

// GzipDecompress 解压缩输入的[]byte数据并返回解压缩后的[]byte数据
func GzipDecompress(data []byte) ([]byte, error) {
	buf := bytes.NewReader(data)
	gzipReader, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	result, err := ioutil.ReadAll(gzipReader)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func IsEOF(err error) bool {
	if strings.Contains(err.Error(), "EOF") {
		return true
	}
	return false
}
