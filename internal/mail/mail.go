// Package mail composes StreamNext's emails and delivers them through a Transport:
// LogTransport prints them (local development only), Resend sends them for real.
package mail

import (
	"context"
	"fmt"
)

// Message is one outgoing plain-text email.
type Message struct {
	To, Subject, Text string
}

// Transport delivers a Message.
type Transport interface {
	Send(ctx context.Context, m Message) error
}

// Mailer writes the app's emails and hands them to a Transport. It implements service.Mailer.
type Mailer struct{ transport Transport }

func New(t Transport) *Mailer { return &Mailer{transport: t} }

func (m *Mailer) SendVerification(ctx context.Context, to, name, link string) error {
	return m.transport.Send(ctx, Message{
		To:      to,
		Subject: "Verify your StreamNext email",
		Text: fmt.Sprintf("Hi %s,\n\nConfirm your email address to finish setting up StreamNext:\n\n%s\n\n"+
			"The link works once and expires in 24 hours. If you didn't create an account, you can ignore this email.\n\n— StreamNext\n", name, link),
	})
}

func (m *Mailer) SendPasswordReset(ctx context.Context, to, name, link string) error {
	return m.transport.Send(ctx, Message{
		To:      to,
		Subject: "Reset your StreamNext password",
		Text: fmt.Sprintf("Hi %s,\n\nSomeone asked to reset the password for your StreamNext account. Choose a new one here:\n\n%s\n\n"+
			"The link works once and expires in 1 hour. If you didn't ask for this, ignore this email: your password hasn't changed.\n\n— StreamNext\n", name, link),
	})
}
