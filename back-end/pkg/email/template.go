package email

import (
	"bytes"
	_ "embed"
	"html/template"
)

//go:embed template/verification_email.html
var verificationTmpl string

func BuildVerificationEmailHTML(code string, expiryMinutes int) string {
	tmpl := template.Must(template.New("email").Parse(verificationTmpl))
	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]any{"Code": code, "ExpiryMinutes": expiryMinutes})
	return buf.String()
}
