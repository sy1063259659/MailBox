package imapmail

import (
	"bytes"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"

	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

type parsedBody struct {
	HTML string
	Text string
}

var htmlHintRE = regexp.MustCompile(`(?is)<(html|body|div|table|p|br|span|style|head|meta|title|!doctype)[\s>]`)

func parseMessageBody(raw []byte) parsedBody {
	if len(raw) == 0 {
		return parsedBody{}
	}

	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return parsedBody{Text: string(raw)}
	}

	var body parsedBody
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		contentType, _, err := inline.ContentType()
		if err != nil {
			contentType = "text/plain"
		}

		data, err := io.ReadAll(part.Body)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(decodeQuotedPrintable(string(data)))
		switch strings.ToLower(contentType) {
		case "text/html":
			if body.HTML == "" {
				body.HTML = text
			}
		case "text/plain":
			if body.Text == "" {
				body.Text = text
			}
		}
	}

	if body.HTML == "" && htmlHintRE.MatchString(body.Text) {
		body.HTML = body.Text
		body.Text = ""
	}

	return body
}

func decodeQuotedPrintable(value string) string {
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(value)))
	if err != nil {
		return value
	}
	return string(decoded)
}
