package signup

// This file declares what the signup entity requires from the application,
// rather than importing it. Both ports are satisfied structurally by the
// concrete clients in cmd/api (internal/sms and internal/mailer), so no adapter
// is needed at the wiring site.

// SmsSender delivers a text message.
//
// The parameters are the recipient's phone number in normalised form and the
// message body. Signup uses this for the pincode that validates a contact
// number, so a failure here is a failure of the signup flow, not a background
// concern.
type SmsSender interface {
	Send(phone, body string) error
}

// Mailer renders a template and delivers it, returning the message ID assigned
// by the transport.
//
// The message ID matters: signup publishes it on the stream as evidence that a
// given mail was sent, which is what lets a support request be traced to a
// specific delivery.
type Mailer interface {
	Send(recipient, templateFile string, data any) (messageID string, err error)
}
