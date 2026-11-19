package mail

import (
	"context"

	"go.uber.org/zap"
)

// LogTransport prints emails instead of sending them, so the whole flow can be tried locally with no
// mail provider. The text contains working tokens, which is why config refuses it when ENV is prod.
type LogTransport struct{ log *zap.Logger }

func NewLogTransport(log *zap.Logger) *LogTransport { return &LogTransport{log: log} }

func (t *LogTransport) Send(_ context.Context, m Message) error {
	t.log.Info("email (log provider, dev only)", zap.String("to", m.To), zap.String("subject", m.Subject), zap.String("text", m.Text))
	return nil
}
