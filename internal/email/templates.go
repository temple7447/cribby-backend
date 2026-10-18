package email

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// Branded transactional emails. One shared layout in Cribby's look (paper background,
// ink outlines, hard offset "sticker" shadow, coral/violet/sunshine/mint accents) drives
// every email, so they all stay consistent. Built with email-client constraints in mind:
// table layout, inline styles only, no web fonts, no JavaScript, and a plain-text
// alternative for every message. All user-supplied text is HTML-escaped by html/template.

const (
	brandCoral    = "#FF6B4A"
	brandViolet   = "#7C5CFF"
	brandSunshine = "#FFC93C"
	brandMint     = "#3ECF8E"
	brandInk      = "#1A1A2E"

	logoURL     = "https://www.cribby.space/logo-mark.png"
	websiteURL  = "https://www.cribby.space"
	supportURL  = "https://www.cribby.space/support"
	privacyURL  = "https://www.cribby.space/privacy"
	adminURL    = "https://admin.cribby.space/bookings"
	fontStack   = "'Baloo 2','Trebuchet MS','Segoe UI',Helvetica,Arial,sans-serif"
	bodyFontSet = "'Plus Jakarta Sans','Segoe UI',Helvetica,Arial,sans-serif"
)

// Message is a ready-to-send email.
type Message struct {
	Subject string
	HTML    string
	Text    string
}

type kv struct{ Label, Value string }

// view is everything the shared layout can show; empty fields are simply omitted.
type view struct {
	Subject   string
	Preheader string // the grey preview line shown next to the subject in inboxes
	Emoji     string
	Band      template.CSS // hero band colour
	Title     string
	Intro     string
	Code      []string // one entry per digit
	CodeNote  string   // e.g. "Expires in 30 minutes."
	Rows      []kv
	Callout   string // a highlighted note box
	Button    string
	ButtonURL string
	Outro     string
	Reason    string // footer: why they received this
	Logo      string
	Support   string
	Privacy   string
	Website   string
	// styles shared by the template
	Coral, Violet, Sunshine, Mint, Ink template.CSS
	Font, BodyFont                     template.CSS
}

func newView() view {
	return view{
		Logo: logoURL, Support: supportURL, Privacy: privacyURL, Website: websiteURL,
		Coral: brandCoral, Violet: brandViolet, Sunshine: brandSunshine, Mint: brandMint, Ink: brandInk,
		Font: fontStack, BodyFont: bodyFontSet,
	}
}

var layout = template.Must(template.New("email").Parse(`<!DOCTYPE html>
<html lang="en" xmlns="http://www.w3.org/1999/xhtml">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light only">
<meta name="supported-color-schemes" content="light only">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background:#FFF9F2;-webkit-text-size-adjust:100%;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:#FFF9F2;font-size:1px;line-height:1px;">{{.Preheader}}&#8199;&#847;&#8199;&#847;&#8199;&#847;&#8199;&#847;&#8199;&#847;&#8199;&#847;</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#FFF9F2;">
<tr><td align="center" style="padding:28px 14px 40px;">

  <!-- wordmark -->
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:560px;">
  <tr><td style="padding:0 4px 18px;">
    <table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
      <td style="padding-right:10px;"><img src="{{.Logo}}" width="36" height="36" alt="" style="display:block;border:0;border-radius:10px;"></td>
      <td style="font-family:{{.Font}};font-size:26px;font-weight:800;color:{{.Coral}};letter-spacing:-0.5px;">Cribby</td>
    </tr></table>
  </td></tr>
  </table>

  <!-- card with hard offset shadow -->
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:560px;">
  <tr><td style="background:{{.Ink}};border-radius:22px;padding:0 6px 6px 0;">
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#FFFFFF;border:2px solid {{.Ink}};border-radius:20px;overflow:hidden;">

      <!-- hero band -->
      <tr><td align="center" style="background:{{.Band}};padding:30px 24px 26px;border-bottom:2px solid {{.Ink}};">
        <div style="font-size:46px;line-height:52px;">{{.Emoji}}</div>
        <div style="margin-top:10px;font-family:{{.Font}};font-size:27px;line-height:32px;font-weight:800;color:{{.Ink}};">{{.Title}}</div>
      </td></tr>

      <tr><td style="padding:28px 20px 8px;font-family:{{.BodyFont}};font-size:16px;line-height:25px;color:{{.Ink}};">
        {{if .Intro}}<p style="margin:0 0 20px;">{{.Intro}}</p>{{end}}

        {{if .Code}}
        <table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0" style="margin:6px auto 12px;"><tr>
          {{range .Code}}<td style="padding:0 2px;"><table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr><td style="background:{{$.Ink}};border-radius:12px;padding:0 3px 3px 0;"><table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr><td width="38" height="54" align="center" style="width:38px;height:54px;background:#FFF9F2;border:2px solid {{$.Ink}};border-radius:11px;font-family:'SFMono-Regular',Menlo,Consolas,'Courier New',monospace;font-size:27px;font-weight:800;color:{{$.Ink}};">{{.}}</td></tr></table></td></tr></table></td>{{end}}
        </tr></table>
        {{if .CodeNote}}<p style="margin:0 0 20px;text-align:center;font-size:14px;color:#6B6B80;">{{.CodeNote}}</p>{{end}}
        {{end}}

        {{if .Rows}}
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 22px;background:#FFF9F2;border:2px solid {{.Ink}};border-radius:14px;">
          {{range $i, $r := .Rows}}<tr>
            <td style="padding:12px 16px;{{if $i}}border-top:1px solid rgba(26,26,46,0.12);{{end}}font-size:13px;font-weight:700;color:#6B6B80;text-transform:uppercase;letter-spacing:0.6px;" width="36%" valign="top">{{$r.Label}}</td>
            <td style="padding:12px 16px;{{if $i}}border-top:1px solid rgba(26,26,46,0.12);{{end}}font-size:15px;font-weight:700;color:{{$.Ink}};" valign="top">{{$r.Value}}</td>
          </tr>{{end}}
        </table>
        {{end}}

        {{if .Callout}}
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 22px;"><tr>
          <td style="background:#FFF4CC;border:2px solid {{.Ink}};border-radius:14px;padding:14px 16px;font-size:14px;line-height:21px;color:{{.Ink}};">{{.Callout}}</td>
        </tr></table>
        {{end}}

        {{if .Button}}
        <table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0" style="margin:8px auto 22px;"><tr>
          <td style="background:{{.Ink}};border-radius:999px;padding:0 4px 4px 0;">
            <table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
              <td align="center" style="background:{{.Coral}};border:2px solid {{.Ink}};border-radius:999px;">
                <a href="{{.ButtonURL}}" style="display:inline-block;padding:14px 32px;font-family:{{.Font}};font-size:18px;font-weight:800;color:#FFFFFF;text-decoration:none;">{{.Button}}</a>
              </td>
            </tr></table>
          </td>
        </tr></table>
        {{end}}

        {{if .Outro}}<p style="margin:0 0 20px;font-size:14px;line-height:22px;color:#55556B;">{{.Outro}}</p>{{end}}
      </td></tr>
    </table>
  </td></tr>
  </table>

  <!-- footer -->
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:560px;">
  <tr><td align="center" style="padding:22px 12px 0;font-family:{{.BodyFont}};font-size:12px;line-height:19px;color:#8A8A9C;">
    {{if .Reason}}{{.Reason}}<br>{{end}}
    <a href="{{.Website}}" style="color:#8A8A9C;text-decoration:underline;">Cribby</a> &middot; student housing &amp; roommates<br>
    <a href="{{.Support}}" style="color:#8A8A9C;text-decoration:underline;">Help &amp; support</a> &middot; <a href="{{.Privacy}}" style="color:#8A8A9C;text-decoration:underline;">Privacy</a>
  </td></tr>
  </table>

</td></tr>
</table>
</body>
</html>`))

// render builds the HTML and a matching plain-text version from the same view.
func render(v view) Message {
	var buf bytes.Buffer
	if err := layout.Execute(&buf, v); err != nil {
		// The template is static and the inputs are plain strings, so this can't happen
		// in practice; fall back to plain text rather than failing to send.
		return Message{Subject: v.Subject, HTML: "<p>" + template.HTMLEscapeString(plainText(v)) + "</p>", Text: plainText(v)}
	}
	return Message{Subject: v.Subject, HTML: buf.String(), Text: plainText(v)}
}

func plainText(v view) string {
	var b strings.Builder
	b.WriteString(v.Title + "\n\n")
	if v.Intro != "" {
		b.WriteString(v.Intro + "\n\n")
	}
	if len(v.Code) > 0 {
		b.WriteString("    " + strings.Join(v.Code, " ") + "\n")
		if v.CodeNote != "" {
			b.WriteString("    " + v.CodeNote + "\n")
		}
		b.WriteString("\n")
	}
	for _, r := range v.Rows {
		fmt.Fprintf(&b, "%s: %s\n", r.Label, r.Value)
	}
	if len(v.Rows) > 0 {
		b.WriteString("\n")
	}
	if v.Callout != "" {
		b.WriteString(v.Callout + "\n\n")
	}
	if v.Button != "" {
		fmt.Fprintf(&b, "%s: %s\n\n", v.Button, v.ButtonURL)
	}
	if v.Outro != "" {
		b.WriteString(v.Outro + "\n\n")
	}
	b.WriteString("--\nCribby · student housing & roommates\n" + v.Website + "\nHelp: " + v.Support + "\n")
	if v.Reason != "" {
		b.WriteString(v.Reason + "\n")
	}
	return b.String()
}

// prettyDate turns "2026-12-01" into "Tue, Dec 1, 2026"; anything else is shown as given.
func prettyDate(s string) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("Mon, Jan 2, 2006")
	}
	return s
}

func digits(code string) []string {
	out := make([]string, 0, len(code))
	for _, r := range code {
		out = append(out, string(r))
	}
	return out
}

func minutesText(n int) string {
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

// VerificationEmail carries the 6-digit code that confirms a student's email address.
func VerificationEmail(code string, minutes int) Message {
	v := newView()
	v.Subject = "Your Cribby verification code"
	v.Preheader = "Use this code to verify your email — it expires in " + minutesText(minutes) + "."
	v.Emoji, v.Band = "✉️", brandSunshine
	v.Title = "Verify your email"
	v.Intro = "Welcome to Cribby! Enter this code in the app to confirm your email address and unlock roommate matching and messaging."
	v.Code = digits(code)
	v.CodeNote = "This code expires in " + minutesText(minutes) + "."
	v.Callout = "🔒 Cribby will never ask you for this code by phone, text or chat. Don't share it with anyone."
	v.Outro = "Didn't create a Cribby account? You can safely ignore this email — no account will be verified."
	v.Reason = "You're receiving this because someone used this email address to sign up for Cribby."
	return render(v)
}

// PasswordResetEmail carries the 6-digit code for the forgot-password flow.
func PasswordResetEmail(code string, minutes int) Message {
	v := newView()
	v.Subject = "Reset your Cribby password"
	v.Preheader = "Your password reset code — it expires in " + minutesText(minutes) + "."
	v.Emoji, v.Band = "🔑", brandViolet
	v.Title = "Reset your password"
	v.Intro = "We got a request to reset the password on your Cribby account. Enter this code in the app to choose a new one."
	v.Code = digits(code)
	v.CodeNote = "This code expires in " + minutesText(minutes) + "."
	v.Callout = "🔒 Cribby will never ask you for this code. If anyone does, it's a scam — don't share it."
	v.Outro = "Didn't ask to reset your password? Ignore this email — your password stays exactly as it is."
	v.Reason = "You're receiving this because a password reset was requested for your Cribby account."
	return render(v)
}

// BookingRequestEmail tells an admin (standing in for the landlord) that a student
// has asked to book a home.
func BookingRequestEmail(student, listing, moveIn string) Message {
	v := newView()
	v.Subject = "New booking request: " + listing
	v.Preheader = student + " wants to book " + listing + " — review the request."
	v.Emoji, v.Band = "📋", brandMint
	v.Title = "New booking request"
	v.Intro = "A student has asked to book a home on Cribby. Review it and approve or reject it from the admin dashboard."
	v.Rows = []kv{{"Student", student}, {"Home", listing}, {"Move-in", prettyDate(moveIn)}, {"Status", "Waiting for your decision"}}
	v.Button, v.ButtonURL = "Review request", adminURL
	v.Outro = "The student is notified by email as soon as you decide."
	v.Reason = "You're receiving this because you're a Cribby admin."
	return render(v)
}

// BookingDecisionEmail tells a student whether their booking request was approved.
func BookingDecisionEmail(listing, status string) Message {
	v := newView()
	v.Button, v.ButtonURL = "Open Cribby", websiteURL
	v.Reason = "You're receiving this because you requested a booking on Cribby."
	if status == "approved" {
		v.Subject = "Your booking request was approved 🎉"
		v.Preheader = "Good news — " + listing + " is yours to move into. Here's what happens next."
		v.Emoji, v.Band = "🎉", brandMint
		v.Title = "You're approved!"
		v.Intro = "Great news — your request for " + listing + " was approved."
		v.Rows = []kv{{"Home", listing}, {"Status", "Approved ✓"}}
		v.Callout = "Next steps: open the app to follow your move-in checklist — guarantor, lease, deposit and keys. Never pay a deposit outside the Cribby app."
		v.Outro = "Congratulations, and welcome to your new place!"
	} else {
		v.Subject = "An update on your booking request"
		v.Preheader = "Your request for " + listing + " wasn't approved this time — here are your next steps."
		v.Emoji, v.Band = "🏡", brandViolet
		v.Title = "Not this time"
		v.Intro = "Thanks for your interest in " + listing + ". Unfortunately your request wasn't approved this time."
		v.Rows = []kv{{"Home", listing}, {"Status", "Not approved"}}
		v.Callout = "Don't give up — there are plenty of other homes near your school. Open the app to keep searching, and try a roommate match too."
		v.Button = "Find another home"
	}
	return render(v)
}
