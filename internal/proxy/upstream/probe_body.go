package upstream

import (
	"encoding/base64"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/repository"
)

// maxBodyText bounds the excerpt of an upstream's error answer in the test's result, as the screen's notice of a failed answer
// bounds a body (R-UI-216).
const maxBodyText = 4000

// redacted stands where the repository's credential stood in an upstream's answer.
const redacted = "[redacted]"

// answerBody is the excerpt of an upstream's answer to the probe that the result carries (Q-REPO-489): for an error status of the
// upstream itself, the text a person reads to find the cause ("repository not found", a proxy's error page). Only a textual answer
// is kept as text, at most maxBodyText bytes cut at a character, and every value of the repository's credential in it is replaced
// (an upstream that echoes the request's headers). The body of a token service's answer is never read into a result: it holds the
// token.
func answerBody(contentType string, body []byte, read int, credential []string) *repository.UpstreamAnswerBody {
	b := &repository.UpstreamAnswerBody{ContentType: contentType, Bytes: read}
	if !textual(contentType) || !utf8.Valid(body) {
		return b
	}
	text := string(body)
	if len(text) > maxBodyText {
		cut := maxBodyText
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, b.Cut = text[:cut], true
	}
	for _, v := range credential {
		text = strings.ReplaceAll(text, v, redacted)
	}
	b.Text = text
	return b
}

// textual reports whether an answer of this media type is text a person can read: text/*, JSON, XML and their kin. An answer
// without a type is read as text when it is valid UTF-8.
func textual(contentType string) bool {
	if contentType == "" {
		return true
	}
	t, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(t, "text/") || strings.HasSuffix(t, "json") || strings.HasSuffix(t, "xml") ||
		t == "application/javascript" || t == "application/x-www-form-urlencoded"
}

// credentialValues are the values of the repository's credential as they could stand in an answer: the Authorization header, its
// token or Base64 part, and a Basic credential's password. Empty without a credential.
func (c *Client) credentialValues() []string {
	h := c.cred.headers().Get("Authorization")
	if h == "" {
		return nil
	}
	out := []string{h}
	scheme, v, _ := strings.Cut(h, " ")
	if v == "" {
		return out
	}
	out = append(out, v)
	if strings.EqualFold(scheme, "basic") {
		if raw, err := base64.StdEncoding.DecodeString(v); err == nil {
			if _, pw, ok := strings.Cut(string(raw), ":"); ok && pw != "" {
				out = append(out, pw)
			}
		}
	}
	return out
}
