package eml

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"time"
)

type Message struct {
	Headers []byte
	Body    []byte

	// from headers
	ParsedHeaders map[string][]string // all headers

	MessageID   string
	Date        time.Time
	Sender      Address
	From        []Address
	ReplyTo     []Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	ContentType string
	Comments    []string
	Keywords    []string
	InReply     []string
	References  []string

	// from body
	Text        string
	Html        string
	Attachments []Attachment
	Parts       []Part
}

type Attachment struct {
	Filename string
	Data     []byte
}

func Parse(data []byte, ignoreErrors bool) (msg Message, err error, bodyParsingErrors []error) {

	// treat the raw data
	raw, err := ParseRaw(data)
	if err != nil {
		return
	}

	// proccess the message headers and body parts
	msg, err, bodyParsingErrors = handleMessage(raw, ignoreErrors)

	// append the body and headers at the message
	msg.Body = raw.Body
	msg.Headers = extractHeaders(&raw.Body, &data)

	return
}

// extract the data from each header and parse the body contents
func handleMessage(r RawMessage, ignoreErrors bool) (msg Message, err error, bodyParsingErrors []error) {

	// proccess and append the headers parameters
	msg.ParsedHeaders = make(map[string][]string)
	for _, rh := range r.RawHeaders {

		// add this header to the parsed headers map
		if _, ok := msg.ParsedHeaders[string(rh.Key)]; !ok {
			msg.ParsedHeaders[string(rh.Key)] = []string{}
		}

		msg.ParsedHeaders[string(rh.Key)] = append(msg.ParsedHeaders[string(rh.Key)], string(rh.Value))

		// handle key headers
		switch strings.ToLower(string(rh.Key)) {
		case `content-type`:
			msg.ContentType = string(rh.Value)
		case `message-id`:
			v := bytes.TrimSpace(rh.Value)
			v = bytes.Trim(rh.Value, `<>`)
			msg.MessageID = string(v)
		case `in-reply-to`:
			ids := strings.Fields(string(rh.Value))
			for _, id := range ids {
				msg.InReply = append(msg.InReply, strings.Trim(id, `<> `))
			}
		case `references`:
			ids := strings.Fields(string(rh.Value))
			for _, id := range ids {
				msg.References = append(msg.References, strings.Trim(id, `<> `))
			}
		case `date`:
			msg.Date = ParseDate(string(rh.Value))
		case `from`:
			msg.From, err = parseAddressList(rh.Value)
		case `sender`:
			msg.Sender, err = ParseAddress(rh.Value)
		case `reply-to`:
			msg.ReplyTo, err = parseAddressList(rh.Value)
		case `to`:
			msg.To, err = parseAddressList(rh.Value)
		case `cc`:
			msg.Cc, err = parseAddressList(rh.Value)
		case `bcc`:
			msg.Bcc, err = parseAddressList(rh.Value)
		case `subject`:
			subject, e := Decode(rh.Value)
			err = e
			msg.Subject = string(subject)
		case `comments`:
			msg.Comments = append(msg.Comments, string(rh.Value))
		case `keywords`:
			ks := strings.Split(string(rh.Value), ",")
			for _, k := range ks {
				msg.Keywords = append(msg.Keywords, strings.TrimSpace(k))
			}
		}

		if err != nil && !ignoreErrors {
			return
		}
	}

	// if no sender header was found, use the first value of From
	if msg.Sender == nil && len(msg.From) > 0 {
		msg.Sender = msg.From[0]
	}

	// do the body parsing
	if msg.ContentType != `` {

		// try to parse the body contents with the passed content type
		parts, e := parseBody(msg.ContentType, r.Body)
		if e != nil {
			msg.Text = string(r.Body) // set the whole message body as the message text
			bodyParsingErrors = append(bodyParsingErrors, e)
			return
		}

		// handle each message part
		for _, part := range parts {
			switch {
			case strings.Contains(part.Type, "text/plain"):
				data, e := UTF8(part.Charset, part.Data)
				if e != nil {
					msg.Text = string(part.Data)
				} else {
					msg.Text = string(data)
				}

				//
			case strings.Contains(part.Type, "text/html"):
				data, e := UTF8(part.Charset, part.Data)
				if e != nil {
					msg.Html = string(part.Data)
				} else {
					msg.Html = string(data)
				}

				//
			default:
				if cd, ok := part.Headers["Content-Disposition"]; ok {
					if strings.Contains(cd[0], "attachment") {
						filename := regexp.MustCompile("(?msi)name=\"(.*?)\"").FindStringSubmatch(cd[0]) //.FindString(cd[0])
						if len(filename) < 2 {
							bodyParsingErrors = append(bodyParsingErrors, fmt.Errorf("failed get filename from header Content-Disposition"))
							break
						}

						dfilename, e := Decode([]byte(filename[1]))
						if e != nil {
							bodyParsingErrors = append(bodyParsingErrors, fmt.Errorf("failed decode filename of attachment [msg: %v]", e))
						} else {
							filename[1] = string(dfilename)
						}

						if encoding, ok := part.Headers["Content-Transfer-Encoding"]; ok {
							switch strings.ToLower(encoding[0]) {
							case "base64":
								part.Data, e = base64.StdEncoding.DecodeString(string(part.Data))
								if e != nil {
									bodyParsingErrors = append(bodyParsingErrors, fmt.Errorf("failed decode base64 [msg: %v]", e))
								}
							case "quoted-printable":
								part.Data, _ = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(part.Data)))
							}
						}

						msg.Attachments = append(msg.Attachments, Attachment{filename[1], part.Data})
					}
				}
			}
		}

		msg.Parts = parts
		msg.ContentType = parts[0].Type
		msg.Text = string(parts[0].Data)
	} else {
		msg.Text = string(r.Body)
	}

	return
}

// get the headers from the full message and sanitize its suffix
func extractHeaders(body *[]byte, data *[]byte) []byte {

	// replace the body from the full message to get just the headers
	headers := bytes.Replace(*data, *body, nil, 1)

	// define a list of CF + LF variations at the headers end
	trimOut := [][]byte{
		[]byte("\n\r\n"),
		[]byte("\r\n\n"),
		[]byte("\r\n"),
		[]byte("\n\r"),
		[]byte("\n"),
		[]byte("\r"),
	}

	// trum each item of the list above from the headers suffix
	for _, i := range trimOut {
		headers = bytes.TrimSuffix(headers, i)
	}

	return headers
}
