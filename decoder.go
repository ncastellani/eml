package eml

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/paulrosania/go-charset/charset"
	goCharset "golang.org/x/net/html/charset"
)

func UTF8(cs string, data []byte) ([]byte, error) {
	if strings.ToUpper(cs) == "UTF-8" {
		return data, nil
	}

	r, err := charset.NewReader(cs, bytes.NewReader(data))
	if err != nil {
		return []byte{}, err
	}

	return io.ReadAll(r)

}

func Decode(bstr []byte) (p []byte, err error) {
	header, err := decodeRFC2047(bstr)
	if err != nil {
		return bstr, nil
	}

	return header, err
}

func decodeRFC2047(d []byte) (r []byte, err error) {
	dec := new(mime.WordDecoder)
	p, err := dec.DecodeHeader(string(d))
	if err != nil {
		return d, nil
	}

	return []byte(p), err
}

func DecodeString(s string) (o string, err error) {
	CharsetReader := func(label string, input io.Reader) (io.Reader, error) {
		label = strings.Replace(label, "windows-", "cp", -1)
		enc, _ := goCharset.Lookup(label)
		return enc.NewDecoder().Reader(input), nil
	}

	mimeDecoder := mime.WordDecoder{CharsetReader: CharsetReader}
	decodedHeader, err := mimeDecoder.DecodeHeader(s)

	if err != nil {
		return decodedHeader, fmt.Errorf("cannot decode MIME-word-encoded header %q: %w", s, err)
	}

	return decodedHeader, nil
}
