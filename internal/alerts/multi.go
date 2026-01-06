package alerts

import (
	"context"
	"fmt"
)

// MultiSender sends alerts to multiple destinations
type MultiSender struct {
	senders []Sender
}

// NewMultiSender creates a new multi-sender
func NewMultiSender(senders ...Sender) *MultiSender {
	return &MultiSender{
		senders: senders,
	}
}

// Send sends the alert to all configured senders
func (s *MultiSender) Send(ctx context.Context, payload *AlertPayload) error {
	var errs []error
	for i, sender := range s.senders {
		if err := sender.Send(ctx, payload); err != nil {
			errs = append(errs, fmt.Errorf("sender %d: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("multi-sender errors: %v", errs)
	}

	return nil
}
