package outlook

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

var wordDecoder = &mime.WordDecoder{
	CharsetReader: charsetReader,
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	cs := strings.ToLower(strings.TrimSpace(charset))
	switch cs {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return input, nil
	case "gbk", "gb2312", "cp936", "windows-936":
		return simplifiedchinese.GBK.NewDecoder().Reader(input), nil
	case "gb18030":
		return simplifiedchinese.GB18030.NewDecoder().Reader(input), nil
	case "big5", "cp950":
		return traditionalchinese.Big5.NewDecoder().Reader(input), nil
	case "iso-8859-1", "latin1":
		return charmap.ISO8859_1.NewDecoder().Reader(input), nil
	case "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(input), nil
	default:
		// Fallback to raw bytes so we never fail the whole message on an exotic charset.
		return input, nil
	}
}

// DecodeHeader decodes an RFC 2047 encoded header value.
func DecodeHeader(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	decoded, err := wordDecoder.DecodeHeader(v)
	if err != nil {
		return v
	}
	return decoded
}

// ParseAddress extracts a display name and email address from a From/To header.
func ParseAddress(raw string) (name, address string) {
	decoded := DecodeHeader(raw)
	if decoded == "" {
		return "", ""
	}
	if addr, err := mail.ParseAddress(decoded); err == nil && addr != nil {
		return strings.TrimSpace(addr.Name), strings.TrimSpace(addr.Address)
	}
	// Fallback: look for <email@domain>
	if start := strings.LastIndex(decoded, "<"); start >= 0 {
		if end := strings.LastIndex(decoded, ">"); end > start {
			name = strings.Trim(strings.TrimSpace(decoded[:start]), `"'`)
			address = strings.TrimSpace(decoded[start+1 : end])
			return name, address
		}
	}
	return "", decoded
}

// ParseDate parses RFC5322 / ISO-8601 email dates into a Unix timestamp.
func ParseDate(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if t, err := mail.ParseDate(raw); err == nil {
		return t.Unix()
	}
	// Strip trailing timezone comments like "(UTC)" or "(CST)"
	if idx := strings.Index(raw, "("); idx > 0 {
		if t, err := mail.ParseDate(strings.TrimSpace(raw[:idx])); err == nil {
			return t.Unix()
		}
	}
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// ParseRFC822 parses raw RFC822 message bytes into a MessageDetail.
func ParseRFC822(id, mailbox, method string, raw []byte) MessageDetail {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		text := string(raw)
		return buildMessageDetail(id, mailbox, method, "", "", mailbox, "", "", time.Now().Unix(), text, "", false)
	}

	subject := DecodeHeader(msg.Header.Get("Subject"))
	fromName, fromAddr := ParseAddress(msg.Header.Get("From"))
	if fromAddr == "" {
		fromAddr = DecodeHeader(msg.Header.Get("From"))
	}
	toHeader := DecodeHeader(msg.Header.Get("To"))
	if toHeader == "" {
		toHeader = mailbox
	}
	ccHeader := DecodeHeader(msg.Header.Get("Cc"))
	createdAt := ParseDate(msg.Header.Get("Date"))
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}

	textBody, htmlBody, hasAttach := extractMIMEParts(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	return buildMessageDetail(id, mailbox, method, subject, fromName, fromAddr, toHeader, ccHeader, createdAt, textBody, htmlBody, hasAttach)
}

func extractMIMEParts(contentType, transferEncoding string, r io.Reader) (textBody, htmlBody string, hasAttach bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			data, _ := io.ReadAll(io.LimitReader(r, 2<<20))
			return string(data), "", false
		}
		mr := multipart.NewReader(r, boundary)
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			disp := strings.ToLower(part.Header.Get("Content-Disposition"))
			if strings.Contains(disp, "attachment") {
				hasAttach = true
				_ = part.Close()
				continue
			}
			pText, pHTML, pAttach := extractMIMEParts(
				part.Header.Get("Content-Type"),
				part.Header.Get("Content-Transfer-Encoding"),
				part,
			)
			_ = part.Close()
			if pAttach {
				hasAttach = true
			}
			if textBody == "" && pText != "" {
				textBody = pText
			}
			if htmlBody == "" && pHTML != "" {
				htmlBody = pHTML
			}
		}
		return textBody, htmlBody, hasAttach
	}

	decodedReader := decodeTransfer(transferEncoding, r)
	if cs := params["charset"]; cs != "" {
		if cr, err := charsetReader(cs, decodedReader); err == nil {
			decodedReader = cr
		}
	}
	data, _ := io.ReadAll(io.LimitReader(decodedReader, 2<<20))
	content := string(data)
	if !utf8.ValidString(content) {
		content = strings.ToValidUTF8(content, "")
	}

	switch mediaType {
	case "text/html":
		return "", content, false
	case "text/plain":
		return content, "", false
	default:
		return "", "", true
	}
}

func decodeTransfer(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		// Strip whitespace so standard base64 decoder handles wrapped lines cleanly.
		raw, _ := io.ReadAll(io.LimitReader(r, 4<<20))
		cleaned := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		return base64.NewDecoder(base64.StdEncoding, bytes.NewReader(cleaned))
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func buildMessageDetail(id, mailbox, method, subject, fromName, fromAddr, to, cc string, createdAt int64, textBody, htmlBody string, hasAttach bool) MessageDetail {
	plain := strings.TrimSpace(textBody)
	if plain == "" && htmlBody != "" {
		plain = StripHTML(htmlBody)
	}
	preview := truncateRunes(strings.Join(strings.Fields(plain), " "), 200)
	code := ExtractCode(subject, textBody, htmlBody)

	var subjPtr, fromNamePtr, codePtr, textPtr, htmlPtr *string
	if subject != "" {
		subjPtr = &subject
	}
	if fromName != "" {
		fromNamePtr = &fromName
	}
	if code != "" {
		codePtr = &code
	}
	if plain != "" {
		textPtr = &plain
	}
	if strings.TrimSpace(htmlBody) != "" {
		htmlPtr = &htmlBody
	}
	if fromAddr == "" {
		fromAddr = "unknown"
	}

	return MessageDetail{
		MessageItem: MessageItem{
			ID:               id,
			Mailbox:          mailbox,
			From:             fromAddr,
			FromName:         fromNamePtr,
			To:               to,
			Subject:          subjPtr,
			IsRead:           true,
			IsStarred:        false,
			HasAttachments:   hasAttach,
			CreatedAt:        createdAt,
			Preview:          preview,
			VerificationCode: codePtr,
			Method:           method,
		},
		Cc:      cc,
		Text:    textPtr,
		HTML:    htmlPtr,
		HasHTML: htmlPtr != nil,
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
